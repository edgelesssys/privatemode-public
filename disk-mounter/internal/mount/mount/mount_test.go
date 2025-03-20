package mount

import (
	"context"
	"log/slog"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetMountPoint(t *testing.T) {
	uuidRxp := regexp.MustCompile(`^/mnt/[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

	testCases := map[string]struct {
		lsblk          stubLsblk
		mount          stubMount
		veritysetup    stubVeritysetup
		wantMountPoint string
		wantErr        bool
	}{
		"not mapped-not mounted": {
			lsblk: stubLsblk{
				names: []string{"/dev/sda", "/dev/sda1", "/dev/sda2"},
			},
			mount:       stubMount{},
			veritysetup: stubVeritysetup{},
		},
		"mapped-not mounted": {
			lsblk: stubLsblk{
				names: []string{"/dev/sda", "/dev/sda1", "/dev/sda2", "/dev/mapper/sda-verity"},
			},
			mount:       stubMount{},
			veritysetup: stubVeritysetup{},
		},
		"mapped-mounted": {
			lsblk: stubLsblk{
				names:      []string{"/dev/sda", "/dev/sda1", "/dev/sda2", "/dev/mapper/sda-verity"},
				mountPoint: "/mnt/data",
			},
			mount:          stubMount{},
			veritysetup:    stubVeritysetup{},
			wantMountPoint: "/mnt/data",
		},
		"partition order does not matter": {
			lsblk: stubLsblk{
				names: []string{"/dev/sda1", "/dev/sda2", "/dev/sda"},
			},
			mount:       stubMount{},
			veritysetup: stubVeritysetup{},
		},
		"other device name formats are handled correctly": {
			lsblk: stubLsblk{
				names: []string{"/dev/loop0", "/dev/loop0p1", "/dev/loop0p2", "/dev/mapper/loop0-verity"},
			},
			mount:       stubMount{},
			veritysetup: stubVeritysetup{},
		},
		"lsblk name errors": {
			lsblk: stubLsblk{
				nameErr: assert.AnError,
			},
			mount:       stubMount{},
			veritysetup: stubVeritysetup{},
			wantErr:     true,
		},
		"lsblk mountpoints errors": {
			lsblk: stubLsblk{
				names:         []string{"/dev/sda", "/dev/sda1", "/dev/sda2"},
				mountPointErr: assert.AnError,
			},
			mount:       stubMount{},
			veritysetup: stubVeritysetup{},
			wantErr:     true,
		},
		"mount errors": {
			lsblk: stubLsblk{
				names: []string{"/dev/sda", "/dev/sda1", "/dev/sda2"},
			},
			mount: stubMount{
				err: assert.AnError,
			},
			veritysetup: stubVeritysetup{},
			wantErr:     true,
		},
		"veritysetup open errors": {
			lsblk: stubLsblk{
				names: []string{"/dev/sda", "/dev/sda1", "/dev/sda2"},
			},
			mount: stubMount{},
			veritysetup: stubVeritysetup{
				openErr: assert.AnError,
			},
			wantErr: true,
		},
		"unexpected third partition name": {
			lsblk: stubLsblk{
				names: []string{"/dev/sda", "/dev/sda1", "/dev/sda2", "/dev/sda3"},
			},
			mount:       stubMount{},
			veritysetup: stubVeritysetup{},
			wantErr:     true,
		},
		"unexpected number of partitions (1)": {
			lsblk: stubLsblk{
				names: []string{"/dev/sda"},
			},
			mount:       stubMount{},
			veritysetup: stubVeritysetup{},
			wantErr:     true,
		},
		"unexpected number of partitions (more than 4)": {
			lsblk: stubLsblk{
				names: []string{"/dev/sda", "/dev/sda1", "/dev/sda2", "/dev/sda3", "/dev/sda4", "/dev/sda5"},
			},
			mount:       stubMount{},
			veritysetup: stubVeritysetup{},
			wantErr:     true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			m := &Mounter{
				lsblk:       tc.lsblk,
				mount:       tc.mount,
				veritysetup: tc.veritysetup,
				log:         slog.Default(),
			}

			gotMountPoint, err := m.MountDisk(t.Context(), "", "")
			if tc.wantErr {
				assert.Error(err)
				return
			}
			assert.NoError(err)
			if tc.wantMountPoint != "" {
				// If we have a specific mount point we want, check that it matches
				assert.Equal(tc.wantMountPoint, gotMountPoint, "unexpected mount point")
			} else {
				// Otherwise, the mount point should be a directory in /mnt with a UUID
				assert.Regexp(uuidRxp, gotMountPoint, "mount point does not match UUID pattern")
			}
		})
	}
}

type stubLsblk struct {
	names         []string
	nameErr       error
	mountPoint    string
	mountPointErr error
}

func (l stubLsblk) Device(_ context.Context, _ string) (BlockDevice, error) {
	return BlockDevice{}, nil
}

func (l stubLsblk) name(_ context.Context, _ string) ([]string, error) {
	return l.names, l.nameErr
}

func (l stubLsblk) mountpoints(_ context.Context, _ string) (string, error) {
	return l.mountPoint, l.mountPointErr
}

type stubMount struct {
	err error
}

func (m stubMount) mount(_, _ string) error {
	return m.err
}

func (m stubMount) unmount(_ string) error {
	return m.err
}

type stubVeritysetup struct {
	openErr  error
	closeErr error
}

func (v stubVeritysetup) open(_ context.Context, _, _, _, _ string) error {
	return v.openErr
}

func (v stubVeritysetup) close(_ context.Context, _ string) error {
	return v.closeErr
}
