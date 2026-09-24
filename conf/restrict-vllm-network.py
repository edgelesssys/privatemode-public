#!/usr/bin/env python3
"""Prepare vLLM's account and restrict its UID to loopback using nftables.

Requires root and CAP_NET_ADMIN, and inet/meta/reject support in the guest kernel's
nftables implementation. The startup wrapper drops privileges only on success.

Wire format: https://docs.kernel.org/netlink/specs/nftables.html
Constants come from Linux's uapi/linux/{netlink.h,netfilter{.h,/*.h}}.
"""

import argparse
import grp
import os
import pwd
import socket
import struct
import subprocess
from pathlib import Path

NETLINK_NETFILTER = 12
NFNL_SUBSYS_NFTABLES = 10
NFNL_MSG_BATCH_BEGIN = 16
NFNL_MSG_BATCH_END = 17
NFPROTO_INET = 1
NF_INET_LOCAL_OUT = 3
NF_ACCEPT = 1
NLMSG_ERROR = 2
NLM_F_REQUEST = 1
NLM_F_ACK = 4
NLM_F_CREATE = 0x400
NLA_F_NESTED = 0x8000

NFT_MSG_NEWTABLE = 0
NFT_MSG_DELTABLE = 2
NFT_MSG_NEWCHAIN = 3
NFT_MSG_NEWRULE = 6
NFT_REG_1 = 1
NFT_META_OIFNAME = 7
NFT_META_SKUID = 10
NFT_CMP_EQ = 0
NFT_CMP_NEQ = 1
NFT_REJECT_ICMPX_UNREACH = 2
NFT_REJECT_ICMPX_PORT_UNREACH = 1

RUNTIME_HOME = Path("/vllm-workspace")


def attr(kind: int, payload: bytes) -> bytes:
    """Netlink attributes have native-endian headers and four-byte alignment."""
    length = 4 + len(payload)
    return struct.pack("=HH", length, kind) + payload + bytes(-length % 4)


def u32(kind: int, value: int) -> bytes:
    # nftables integer attributes use network byte order.
    return attr(kind, struct.pack("!I", value))


def string(kind: int, value: str) -> bytes:
    return attr(kind, value.encode("ascii") + b"\0")


def nested(kind: int, payload: bytes) -> bytes:
    return attr(kind | NLA_F_NESTED, payload)


def expression(name: str, data: bytes) -> bytes:
    # NFTA_LIST_ELEM containing NFTA_EXPR_NAME and NFTA_EXPR_DATA.
    return nested(1, string(1, name) + nested(2, data))


def match_meta(key: int, op: int, value: bytes) -> bytes:
    return expression(
        "meta",
        u32(1, NFT_REG_1) + u32(2, key),  # NFTA_META_DREG, NFTA_META_KEY
    ) + expression(
        "cmp",
        u32(1, NFT_REG_1)  # NFTA_CMP_SREG
        + u32(2, op)  # NFTA_CMP_OP
        + nested(3, attr(1, value)),  # NFTA_CMP_DATA / NFTA_DATA_VALUE
    )


def message(kind: int, flags: int, seq: int, payload: bytes) -> bytes:
    return struct.pack("=IHHII", 16 + len(payload), kind, flags, seq, 0) + payload


def transact(commands: list[tuple[int, bytes]]) -> None:
    """Commit all commands atomically and fail unless every command is ACKed."""
    batch_header = struct.pack("!BBH", socket.AF_UNSPEC, 0, NFNL_SUBSYS_NFTABLES)
    data = message(NFNL_MSG_BATCH_BEGIN, NLM_F_REQUEST, 0, batch_header)
    pending = set()
    for seq, (operation, attributes) in enumerate(commands, 1):
        flags = NLM_F_REQUEST | NLM_F_ACK
        if operation != NFT_MSG_DELTABLE:
            flags |= NLM_F_CREATE
        data += message(
            (NFNL_SUBSYS_NFTABLES << 8) | operation,
            flags,
            seq,
            struct.pack("!BBH", NFPROTO_INET, 0, 0) + attributes,
        )
        pending.add(seq)
    data += message(NFNL_MSG_BATCH_END, NLM_F_REQUEST, len(commands) + 1, batch_header)

    with socket.socket(socket.AF_NETLINK, socket.SOCK_RAW, NETLINK_NETFILTER) as sock:
        sock.settimeout(10)
        sock.bind((0, 0))
        sock.sendto(data, (0, 0))
        while pending:
            reply, _, flags, sender = sock.recvmsg(65536)
            if flags & socket.MSG_TRUNC or sender[0] != 0:
                raise RuntimeError("Truncated or non-kernel nftables reply")
            offset = 0
            while offset < len(reply):
                if len(reply) - offset < 16:
                    raise RuntimeError("Truncated Netlink header")
                length, kind, _, seq, _ = struct.unpack_from("=IHHII", reply, offset)
                if length < 20 or offset + length > len(reply) or kind != NLMSG_ERROR:
                    raise RuntimeError("Unexpected nftables reply")
                error = struct.unpack_from("=i", reply, offset + 16)[0]
                if error:
                    raise OSError(
                        -error, f"nftables command {seq}: {os.strerror(-error)}"
                    )
                pending.discard(seq)
                offset += (length + 3) & ~3


def restrict_network(uid: int) -> None:
    """Install `meta skuid UID oifname != lo reject` for IPv4 and IPv6."""
    table_name = f"privatemode_vllm_{uid}"
    table = string(1, table_name)  # NFTA_TABLE_NAME
    chain = (
        string(1, table_name)  # NFTA_CHAIN_TABLE
        + string(3, "output")  # NFTA_CHAIN_NAME
        + nested(
            4,  # NFTA_CHAIN_HOOK
            u32(1, NF_INET_LOCAL_OUT) + u32(2, 0),  # HOOKNUM, PRIORITY (filter)
        )
        + u32(5, NF_ACCEPT)  # NFTA_CHAIN_POLICY
        + string(7, "filter")  # NFTA_CHAIN_TYPE
    )
    # Register values are raw bytes: skuid is native-endian, and interface names
    # are NUL-padded to IFNAMSIZ (16). Attribute integers above are big-endian.
    expressions = (
        match_meta(NFT_META_SKUID, NFT_CMP_EQ, struct.pack("=I", uid))
        + match_meta(NFT_META_OIFNAME, NFT_CMP_NEQ, b"lo" + bytes(14))
        + expression(
            "reject",
            u32(1, NFT_REJECT_ICMPX_UNREACH)  # NFTA_REJECT_TYPE
            + attr(2, bytes([NFT_REJECT_ICMPX_PORT_UNREACH])),  # ICMP_CODE (u8)
        )
    )
    rule = (
        string(1, table_name)  # NFTA_RULE_TABLE
        + string(2, "output")  # NFTA_RULE_CHAIN
        + nested(4, expressions)  # NFTA_RULE_EXPRESSIONS
    )
    # Rules survive container restarts. Ensure our per-UID table exists, then
    # replace it in the SAME transaction: no unfiltered interval, no duplicates,
    # and no changes to other models' or the pod's tables. A failed transaction
    # leaves the previous rules intact. ACCEPT in another base chain cannot
    # bypass this chain's REJECT.
    transact(
        [
            (NFT_MSG_NEWTABLE, table),
            (NFT_MSG_DELTABLE, table),
            (NFT_MSG_NEWTABLE, table),
            (NFT_MSG_NEWCHAIN, chain),
            (NFT_MSG_NEWRULE, rule),
        ]
    )


def prepare_account(uid: int) -> None:
    """Create the account needed by pwd.getpwuid() and a writable home/cache."""
    name = f"vllm-{uid}"
    try:
        grp.getgrgid(uid)
    except KeyError:
        subprocess.run(["groupadd", "--gid", str(uid), name], check=True)
    try:
        pwd.getpwuid(uid)
    except KeyError:
        subprocess.run(
            [
                "useradd",
                "--uid",
                str(uid),
                "--gid",
                str(uid),
                "--no-create-home",
                "--no-log-init",
                "--home-dir",
                str(RUNTIME_HOME),
                "--shell",
                "/usr/sbin/nologin",
                name,
            ],
            check=True,
        )
    RUNTIME_HOME.mkdir(parents=True, exist_ok=True)
    os.chown(RUNTIME_HOME, uid, uid, follow_symlinks=False)

    def raise_walk_error(error: OSError) -> None:
        raise error

    for root, directories, files in os.walk(RUNTIME_HOME, onerror=raise_walk_error):
        for entry in directories + files:
            os.chown(Path(root) / entry, uid, uid, follow_symlinks=False)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "uid", type=int, help="vLLM UID/GID supplied by the startup wrapper"
    )
    args = parser.parse_args()
    if not 0 < args.uid < 2**32 - 1:
        parser.error("uid must be a non-root UID smaller than 2**32 - 1")
    restrict_network(args.uid)
    prepare_account(args.uid)


if __name__ == "__main__":
    main()
