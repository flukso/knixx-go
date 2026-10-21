//go:build !linux

package uf2

// MountVolume reports ErrNoDrive: macOS mounts the drive as /Volumes/<name>
// by itself.
func MountVolume(names []string) (string, error) {
	return "", ErrNoDrive
}
