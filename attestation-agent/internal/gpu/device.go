//go:build gpu

package gpu

import (
	"fmt"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

// Device represents a GPU device.
type Device struct {
	id           string
	cachedHandle *nvml.Device
}

// ID returns the (NVIDIA) ID of the GPU.
func (d *Device) ID() string {
	return d.id
}

// DeviceInfo holds information about the GPU device.
type DeviceInfo struct {
	Architecture  nvml.DeviceArchitecture
	DriverVersion string
	VBIOSVersion  string
}

// Info returns information about the GPU device.
func (d *Device) Info() (*DeviceInfo, error) {
	device, err := d.handle()
	if err != nil {
		return nil, fmt.Errorf("getting GPU handle: %w", err)
	}

	architecture, ret := device.GetArchitecture()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("getting GPU architecture: %s", nvml.ErrorString(ret))
	}

	vbiosVersion, ret := device.GetVbiosVersion()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("getting GPU VBIOS version: %s", nvml.ErrorString(ret))
	}

	driverVersion, ret := nvml.SystemGetDriverVersion()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("getting GPU driver version: %s", nvml.ErrorString(ret))
	}

	return &DeviceInfo{
		Architecture:  architecture,
		DriverVersion: driverVersion,
		VBIOSVersion:  vbiosVersion,
	}, nil
}

// handle returns the device handle for the GPU.
func (d *Device) handle() (nvml.Device, error) {
	if d.cachedHandle != nil {
		return *d.cachedHandle, nil
	}

	device, ret := nvml.DeviceGetHandleByUUID(d.ID())
	if ret != nvml.SUCCESS {
		return nvml.Device{}, fmt.Errorf("getting GPU handle: %s", nvml.ErrorString(ret))
	}
	d.cachedHandle = &device

	return device, nil
}
