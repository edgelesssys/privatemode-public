//go:build gpu

package gpu

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const callBackInterval = time.Second * 30

/*
An Inventory is the inventory of GPUs of a machine.

Inventory is safe for concurrent accesses.

It is implemented as a singleton because GPU->workload assignment is a state that is global machine-wide and
needs to be consistently used across the worker API.
*/
type Inventory struct {
	mut           *sync.RWMutex
	availableGPUs []Device
	inUseGPUs     map[string][]Device

	callBacks []func(ctx context.Context, user string) error

	log *slog.Logger
}

// inventorySingleton is the process-wide GPU inventory.
var inventorySingleton *Inventory

// NewInventory returns the process-wide GPU inventory, and initializes it if
// it does not exist yet.
func NewInventory(ctx context.Context, availableGPUs []Device, log *slog.Logger) (*Inventory, error) {
	if inventorySingleton != nil {
		return inventorySingleton, nil
	}

	inventorySingleton = &Inventory{
		log:           log,
		mut:           &sync.RWMutex{},
		availableGPUs: availableGPUs,
		inUseGPUs:     make(map[string][]Device),
		callBacks:     nil,
	}

	// Start the callback loop
	go inventorySingleton.callBackLoop(ctx)

	return inventorySingleton, nil
}

// Request requests count GPUs for the name user. It returns an error if not enough GPUs
// are available. If there already are GPUs reserved for the name user, the requested GPUs
// will be added to the existing reservation.
func (i *Inventory) Request(user string, count uint) ([]Device, error) {
	i.mut.Lock()
	defer i.mut.Unlock()

	i.log.Info("Requesting GPUs", "user", user, "count", count)

	if len(i.availableGPUs) < int(count) {
		return nil, fmt.Errorf("not enough GPUs available")
	}

	gpus := i.availableGPUs[:count]
	i.availableGPUs = i.availableGPUs[count:]

	i.log.Info("GPUs currently in use by user", "user", user, "gpus", i.inUseGPUs[user])

	newGPUs := append(i.inUseGPUs[user], gpus...)
	i.inUseGPUs[user] = newGPUs

	i.log.Info("Successfully requested GPUs", "user", user, "count", count, "gpus", gpus)

	return newGPUs, nil
}

// ReleaseAll releases all in-use GPUs reserved for the name user.
// This is a no-op if the user has no GPUs reserved.
func (i *Inventory) ReleaseAll(user string) {
	i.mut.Lock()
	defer i.mut.Unlock()

	i.log.Info("Releasing GPUs", "user", user)

	if len(i.inUseGPUs[user]) == 0 {
		i.log.Info("No GPUs to release", "user", user)
		delete(i.inUseGPUs, user)
		return
	}

	i.availableGPUs = append(i.availableGPUs, i.inUseGPUs[user]...)
	delete(i.inUseGPUs, user)

	i.log.Info("Successfully released GPUs", "user", user)
}

// RegisterCallBack registers a callback function that will be called regularly by the inventory.
func (i *Inventory) RegisterCallBack(cb func(ctx context.Context, user string) error) {
	i.mut.Lock()
	defer i.mut.Unlock()
	i.callBacks = append(i.callBacks, cb)
}

// callBackLoop runs the registered callbacks at regular intervals.
func (i *Inventory) callBackLoop(ctx context.Context) {
	ticker := time.NewTicker(callBackInterval)
	for {
		select {
		case <-ctx.Done():
			i.log.Info("Stopping callback loop")
			return
		case <-ticker.C:
			i.runCallBacks(ctx)
		}
	}
}

// runCallBacks runs all registered callbacks for all users in the inventory.
func (i *Inventory) runCallBacks(ctx context.Context) {
	i.mut.RLock()
	callBacks := make([]func(ctx context.Context, user string) error, len(i.callBacks))
	copy(callBacks, i.callBacks)
	var users []string
	for user := range i.inUseGPUs {
		users = append(users, user)
	}
	i.mut.RUnlock()

	for _, user := range users {
		for _, cb := range callBacks {
			ctx, cancel := context.WithTimeout(ctx, time.Second*30)
			defer cancel()

			if err := cb(ctx, user); err != nil {
				i.log.Error("Error running callback", "error", err)
			}
		}
	}
}
