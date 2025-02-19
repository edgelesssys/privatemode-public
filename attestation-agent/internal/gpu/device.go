package gpu

// Device represents a GPU device.
type Device struct {
	id string
}

// ID returns the (NVIDIA) ID of the GPU.
func (d Device) ID() string {
	return d.id
}
