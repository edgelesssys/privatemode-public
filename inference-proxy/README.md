# Continuum Inference Proxy

Continuum allows users to send encrypted inference requests to inference servers.
To allow running unmodified inference servers, Continuum's worker nodes, which the inference servers are deployed on,
run an encryption proxy that decrypts ingoing requests and encrypts outgoing responses.

To stay compatible with load balancing options, Continuum doesn't encrypt the entire data of the inference request and response.
Instead Continuum implements API adapters that selectively, encrypt only the sensitive parts of requests and responses,
e.g. the `text_input` and `text_output` fields for Triton inference requests and responses.

The inference encryption proxy aims to be compatible with as many different inference protocols as possible,
and is designed to be easily extendable to support new protocols.

```shell
                                                       v
                                             Encrypted v Plain
                                                  Text v Text
                                                       v
                                                   ┌────────┐
                                                   │ cipher │
                                               ┌───┴────────┴───┐
Incoming┌──────────────┐  ┌────────────────┐ ┌─► Triton Adapter ◄─┐ ┌────────┐
 Request│              │  │                ◄─┘ └───┬────────┬───┘ └─►        │  ┌─────────────────┐
 ───────► Third Party  ├──►   Inference    │   ┌───┴────────┴───┐   │  Unix  │  │                 │
        │ Loadbalancer │  │   Encryption   ◄───► ABC Adapter    ◄───► Socket ◄──► Inference Server│
 ◄──────┤              ◄──┤ Proxy Endpoint │   └───┬────────┬───┘   │        │  │                 │
Outgoing│              │  │                ◄─┐     │        │     ┌─►        │  └─────────────────┘
Response└──────────────┘  └────────────────┘ │    ............    │ └────────┘
                                             │                    │
                                             │ ┌───┴────────┴───┐ │
                                             └─► XYZ Adapter    ◄─┘
                                               └───┬────────┬───┘
                                                   └────────┘
                                                       v
                                                       v
```
