package mount

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanTargetPath(t *testing.T) {
	testCases := map[string]struct {
		preparePath func(*require.Assertions, string)
		wantErr     bool
	}{
		"nonexistent": {
			preparePath: func(_ *require.Assertions, _ string) {},
			wantErr:     false,
		},
		"target exists as regular file": {
			preparePath: func(require *require.Assertions, targetPath string) {
				require.NoError(os.WriteFile(targetPath, []byte("content"), 0o644))
			},
			wantErr: true,
		},
		"target exists as symlink": {
			preparePath: func(require *require.Assertions, targetPath string) {
				tmpDir := filepath.Dir(targetPath)
				testFile := filepath.Join(tmpDir, "test")
				require.NoError(os.WriteFile(testFile, []byte("content"), 0o644))
				require.NoError(os.Symlink(testFile, targetPath))
			},
			wantErr: false,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			tmpDir := t.TempDir()
			targetPath := filepath.Join(tmpDir, "target")
			tc.preparePath(require, targetPath)

			err := cleanTargetPath(targetPath)
			if tc.wantErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
			}
		})
	}
}
