//go:build !linux

package mount

type mountSyscall struct{}

func (m mountSyscall) mount(_, _ string) error {
	panic("mount syscall is only supported on Linux")
}

func (m mountSyscall) unmount(_ string) error {
	panic("unmount syscall is only supported on Linux")
}
