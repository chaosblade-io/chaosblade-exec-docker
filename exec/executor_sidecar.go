/*
 * Copyright 1999-2019 Alibaba Group Holding Ltd.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package exec

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/sirupsen/logrus"

	"github.com/chaosblade-io/chaosblade-spec-go/spec"
)

type RunInSidecarContainerExecutor struct {
	BaseDockerClientExecutor
	runConfigFunc func(container string) (container.HostConfig, network.NetworkingConfig)
	isResident    bool
}

func (*RunInSidecarContainerExecutor) Name() string {
	return "runAndExecSidecar"
}

func (r *RunInSidecarContainerExecutor) Exec(uid string, ctx context.Context, expModel *spec.ExpModel) *spec.Response {
	if err := r.SetClient(expModel); err != nil {
		return spec.ReturnFail(spec.Code[spec.DockerInvokeError], err.Error())
	}
	containerId := expModel.ActionFlags[ContainerIdFlag.Name]
	if containerId == "" {
		return spec.ReturnFail(spec.Code[spec.IllegalParameters], "less container id flag")
	}
	hostConfig, networkingConfig := r.runConfigFunc(containerId)
	sidecarName := createSidecarContainerName(containerId, expModel.ActionName)
	return r.startAndExecInContainer(uid, ctx, expModel, &hostConfig, &networkingConfig, sidecarName, r.isResident)
}

func NewNetWorkSidecarExecutor() *RunInSidecarContainerExecutor {
	runConfigFunc := func(containerId string) (container.HostConfig, network.NetworkingConfig) {
		hostConfig := container.HostConfig{
			NetworkMode: container.NetworkMode(fmt.Sprintf("container:%s", containerId)),
			CapAdd:      []string{"NET_ADMIN"},
		}
		networkConfig := network.NetworkingConfig{}
		return hostConfig, networkConfig
	}
	return &RunInSidecarContainerExecutor{
		// set the client when invoking
		runConfigFunc: runConfigFunc,
		isResident:    true,
		BaseDockerClientExecutor: BaseDockerClientExecutor{
			CommandFunc: commonFunc,
		},
	}
}

func createSidecarContainerName(containerId, injectType string) string {
	return fmt.Sprintf("%s-%s", containerId, injectType)
}

func (*RunInSidecarContainerExecutor) SetChannel(channel spec.Channel) {
}

func (r *RunInSidecarContainerExecutor) getContainerConfig(uid string, ctx context.Context, expModel *spec.ExpModel) *container.Config {
	command := r.CommandFunc(uid, ctx, expModel)
	return &container.Config{
		// detach
		AttachStdout: false,
		AttachStderr: false,
		Tty:          true,
		Cmd:          []string{"/bin/sh", "-c", command},
		Image:        getChaosBladeImageRef(expModel.ActionFlags[ImageRepoFlag.Name]),
		Labels: map[string]string{
			"chaosblade": "chaosblade-sidecar",
		},
	}
}

func (r *RunInSidecarContainerExecutor) startAndExecInContainer(uid string, ctx context.Context, expModel *spec.ExpModel,
	hostConfig *container.HostConfig, networkConfig *network.NetworkingConfig,
	containerName string, removed bool) *spec.Response {
	config := r.getContainerConfig(uid, ctx, expModel)

	var sidecarContainerId, output string
	var err error
	var defaultResponse *spec.Response
	var returnedResponse *spec.Response
	// check if the container exists or not
	container0, err := r.Client.getContainerByName(containerName)
	if _, ok := spec.IsDestroy(ctx); ok {
		if err != nil {
			// container not found, wraps err to response
			err = spec.ReturnFail(spec.Code[spec.StatusError], fmt.Sprintf("%v, sidecar container: %s", err, containerName))
		} else {
			sidecarContainerId = container0.ID
			// execute destroy command and remove it if success
			output, err = r.Client.execContainer(sidecarContainerId, config.Cmd[len(config.Cmd)-1])
		}
		if err != nil {
			defaultResponse = spec.ReturnFail(spec.Code[spec.DockerInvokeError], err.Error())
		}
		returnedResponse = ConvertContainerOutputToResponse(output, err, defaultResponse)
		if returnedResponse.Success {
			err = r.Client.forceRemoveContainer(sidecarContainerId)
			logrus.Warningf("force remove container err for destroying, %v", err)
		}
	} else {
		containerCanBeUsed := false
		if err == nil && container0.State == "running" {
			// container exists, use the container to execute the command
			containerCanBeUsed = true
			sidecarContainerId = container0.ID
			output, err = r.Client.execContainer(sidecarContainerId, config.Cmd[len(config.Cmd)-1])
		} else if err == nil {
			// remove
			err = r.Client.forceRemoveContainer(container0.ID)
			logrus.Warningf("remove %s container for network experiment failed, %v", container0.ID, err)
		}
		if !containerCanBeUsed {
			// container not found, start a new container and execute the command
			startConfig := *config
			startConfig.Cmd = getHoldingCommand(startConfig.Cmd)
			sidecarContainerId, output, err = r.Client.executeAndRemove(
				&startConfig, hostConfig, networkConfig, containerName, false, time.Second)
		}
		if err != nil {
			defaultResponse = spec.ReturnFail(spec.Code[spec.DockerInvokeError], err.Error())
		}
		returnedResponse = ConvertContainerOutputToResponse(output, err, defaultResponse)
		// remove the container if failed
		if !containerCanBeUsed && !returnedResponse.Success {
			err = r.Client.forceRemoveContainer(sidecarContainerId)
			logrus.Warningf("force remove container err for creating, %v", err)
		}
	}
	logrus.Infof("sidecarContainerId for experiment %s is %s, output is %s, err is %v", uid, sidecarContainerId, output, err)
	return returnedResponse
}

func getHoldingCommand(commands []string) []string {
	length := len(commands)
	return append(commands[:length-1], commands[length-1]+";bash")
}
