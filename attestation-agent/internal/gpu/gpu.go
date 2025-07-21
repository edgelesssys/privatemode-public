//go:build gpu

/*
Package gpu implements functionality to talk to local NVIDIA GPUs.
*/
package gpu

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

/*
A Client can talk to local NVIDIA GPUs.

One client is safe for concurrent use by multiple goroutines.

It is implemented as a singleton, as the client loads dynamic libraries at runtime and thus
imposes a memory overhead on the process if duplicated.
*/
type Client struct {
	log *slog.Logger
}

// clientSingleton is the process-wide GPU client.
var (
	clientSingleton *Client
	clientCreator   sync.Once
)

/*
NewClient creates and initializes the process-wide GPU client if
it does not exist yet. Otherwise, it returns the existing client.

The caller must call Close() on the client when done using it.
*/
func NewClient(logger *slog.Logger) (*Client, error) {
	clientCreator.Do(func() {
		clientSingleton = &Client{logger}
		err := clientSingleton.init()
		if err != nil {
			logger.Error("Initializing GPU client", "error", err)
		}
	})

	return clientSingleton, nil
}

// init initializes the Client. Internally, it loads the NVML library
// object file.
func (c *Client) init() error {
	c.log.Info("Initializing GPU client")
	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("initializing NVML: %s", nvml.ErrorString(ret))
	}
	return nil
}

// Close closes the Client. Internally, it unloads the NVML library
// object file.
func (c *Client) Close() error {
	c.log.Info("Closing GPU client")
	ret := nvml.Shutdown()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("shutting down NVML: %s", nvml.ErrorString(ret))
	}
	return nil
}

// ListGPUs returns a list of all NVIDIA GPUs in the system.
func (c *Client) ListGPUs() ([]*Device, error) {
	gpuCount, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("getting GPU count: %s", nvml.ErrorString(ret))
	}

	gpus := make([]*Device, gpuCount)
	for i := range gpuCount {
		device, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("getting GPU handle: %s", nvml.ErrorString(ret))
		}

		id, ret := nvml.DeviceGetUUID(device)
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("getting GPU UUID: %s", nvml.ErrorString(ret))
		}

		gpus[i] = &Device{
			id:           id,
			cachedHandle: nil,
		}
	}

	return gpus, nil
}

// SetGPUsReady sets the confidential compute GPUs to ready state.
func (c *Client) SetGPUsReady() error {
	ret := nvml.SystemSetConfComputeGpusReadyState(1)
	if ret != nvml.SUCCESS {
		return fmt.Errorf("setting GPUs ready: %s", nvml.ErrorString(ret))
	}
	return nil
}
