package actions

import (
	"github.com/containrrr/watchtower/internal/util"
	"github.com/containrrr/watchtower/pkg/container"
	log "github.com/sirupsen/logrus"
)

// Update looks at the running Docker containers to see if any of the images
// used to start those containers have been updated. If a change is detected in
// any of the images, the associated containers are stopped and restarted with
// the new image.
func Update(client container.Client, params UpdateParams) error {
	log.Debug("Checking containers for updated images")

	containers, err := client.ListContainers(params.Filter)
	if err != nil {
		return err
	}

	for i, container := range containers {
		stale, err := client.IsContainerStale(container)
		if err != nil {
			log.Infof("Unable to update container %s. Proceeding to next.", containers[i].Name())
			log.Debug(err)
			stale = false
		}
		containers[i].Stale = stale
	}

	containers, err = container.SortByDependencies(containers)
	if err != nil {
		return err
	}

	checkDependencies(containers)

	if params.MonitorOnly {
		return nil
	}

	stopContainersInReversedOrder(containers, client, params)
	restartContainersInSortedOrder(containers, client, params)

	return nil
}

func stopContainersInReversedOrder(containers []container.Container, client container.Client, params UpdateParams) {
	for i := len(containers) - 1; i >= 0; i-- {
		stopStaleContainer(containers[i], client, params)
	}
}

func stopStaleContainer(container container.Container, client container.Client, params UpdateParams) {
	if container.IsWatchtower() {
		log.Debugf("This is the watchtower container %s", container.Name())
		return
	}

	if !container.Stale {
		return
	}

	err := client.StopContainer(container, params.Timeout)
	if err != nil {
		log.Error(err)
	}
}

func restartContainersInSortedOrder(containers []container.Container, client container.Client, params UpdateParams) {
	for _, container := range containers {
		if !container.Stale {
			continue
		}
		restartStaleContainer(container, client, params)
	}
}

func restartStaleContainer(container container.Container, client container.Client, params UpdateParams) {
	// Since we can't shutdown a watchtower container immediately, we need to
	// start the new one while the old one is still running. This prevents us
	// from re-using the same container name so we first rename the current
	// instance so that the new one can adopt the old name.
	if container.IsWatchtower() {
		if err := client.RenameContainer(container, util.RandName()); err != nil {
			log.Error(err)
			return
		}
	}

	if !params.NoRestart {
		if err := client.StartContainer(container); err != nil {
			log.Error(err)
		}
	}

	if params.Cleanup {
		if err := client.RemoveImage(container); err != nil {
			log.Error(err)
		}
	}
}

func checkDependencies(containers []container.Container) {

	for i, parent := range containers {
		if parent.Stale {
			continue
		}

	LinkLoop:
		for _, linkName := range parent.Links() {
			for _, child := range containers {
				if child.Name() == linkName && child.Stale {
					containers[i].Stale = true
					break LinkLoop
				}
			}
		}
	}
}
