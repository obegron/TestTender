package common

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"k8s.io/klog"

	"github.com/obegron/testtender/internal/backend"
	"github.com/obegron/testtender/internal/model/types"
)

// StartContainer will start given container and saves the appropriate state
// in the database.
func StartContainer(cr *ContextRouter, tainr *types.Container) error {
	state, err := cr.Backend.StartContainer(tainr)
	if err != nil {
		return err
	}

	tainr.HostIP = "0.0.0.0"
	if cr.Config.PortForward {
		cr.Backend.CreatePortForwards(tainr)
	} else {
		if len(tainr.GetServicePorts()) > 0 {
			ip, err := cr.Backend.GetPodIP(tainr)
			if err != nil {
				return err
			}
			tainr.HostIP = ip
			if cr.Config.ReverseProxy {
				cr.Backend.CreateReverseProxies(tainr)
			}
		}
	}

	tainr.Stopped = false
	tainr.Killed = false
	tainr.Failed = (state == backend.DeployFailed)
	tainr.Completed = (state == backend.DeployCompleted)
	tainr.Running = (state == backend.DeployRunning)
	if (tainr.Running || tainr.Completed) && tainr.Started.IsZero() {
		tainr.Started = time.Now()
	}
	if tainr.Completed && tainr.Finished.IsZero() {
		tainr.Finished = time.Now()
	}

	return cr.DB.SaveContainer(tainr)
}

// ContainerStateResponse returns the Docker-compatible state shared by the
// Docker and libpod inspect endpoints.
func ContainerStateResponse(tainr *types.Container, relatedError string) gin.H {
	errors := make([]string, 0, 2)
	if relatedError = strings.TrimSpace(relatedError); relatedError != "" {
		errors = append(errors, relatedError)
	}
	if stateError := strings.TrimSpace(tainr.StateError); stateError != "" {
		errors = append(errors, stateError)
	}
	return gin.H{
		"Health": gin.H{
			"Status": tainr.StatusString(),
		},
		"Running":    tainr.Running,
		"Status":     tainr.StateString(),
		"Paused":     false,
		"Restarting": false,
		"OOMKilled":  tainr.OOMKilled,
		"Dead":       tainr.Failed,
		"StartedAt":  tainr.Started.UTC().Format(time.RFC3339Nano),
		"FinishedAt": tainr.Finished.UTC().Format(time.RFC3339Nano),
		"ExitCode":   tainr.ExitCode,
		"Error":      strings.Join(errors, "; "),
	}
}

// UpdateContainerStatus will check if the started container is finished and will
// update the container database record accordingly.
func UpdateContainerStatus(cr *ContextRouter, tainr *types.Container) {
	if tainr.Completed {
		return
	}
	if !cr.Limiter.Allow() {
		klog.V(2).Infof("rate-limited status request for container: %s", tainr.ID)
		return
	}
	status, err := cr.Backend.GetContainerStatus(tainr)
	if err != nil {
		klog.Warningf("container status error: %s", err)
		tainr.Failed = true
	}
	if status == backend.DeployCompleted {
		if tainr.Finished.IsZero() {
			tainr.Finished = time.Now()
		}
		tainr.Completed = true
		tainr.Running = false
	}
}
