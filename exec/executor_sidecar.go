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

	"github.com/chaosblade-io/chaosblade-spec-go/util"
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
		util.Errorf(uid, util.GetRunFuncName(), spec.DockerExecFailed.Sprintf("GetClient", err))
		return spec.ResponseFailWithFlags(spec.DockerExecFailed, "GetClient", err)
	}
	containerId := expModel.ActionFlags[ContainerIdFlag.Name]
	containerName := expModel.ActionFlags[ContainerNameFlag.Name]
	container, response := GetContainer(r.Client, uid, containerId, containerName)
	if !response.Success {
		return response
	}
	hostConfig, networkingConfig := r.runConfigFunc(container.ID)
	sidecarName := createSidecarContainerName(container.Names[0], expModel.Target, expModel.ActionName)
	return r.startAndExecInContainer(uid, ctx, expModel, &hostConfig, &networkingConfig, sidecarName)
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
		isResident:    false,
		BaseDockerClientExecutor: BaseDockerClientExecutor{
			CommandFunc: commonFunc,
		},
	}
}

func createSidecarContainerName(containerName, target, injectType string) string {
	return fmt.Sprintf("%s-%s-%s", containerName, target, injectType)
}

func (*RunInSidecarContainerExecutor) SetChannel(channel spec.Channel) {
}

func (r *RunInSidecarContainerExecutor) getContainerConfig(expModel *spec.ExpModel) *container.Config {
	return &container.Config{
		// detach
		AttachStdout: false,
		AttachStderr: false,
		Tty:          true,
		Cmd:          []string{"/bin/sh"},
		Image: getChaosBladeImageRef(expModel.ActionFlags[ImageRepoFlag.Name],
			expModel.ActionFlags[ImageVersionFlag.Name]),
		Labels: map[string]string{
			"chaosblade": "chaosblade-sidecar",
		},
	}
}

func (r *RunInSidecarContainerExecutor) startAndExecInContainer(uid string, ctx context.Context, expModel *spec.ExpModel,
	hostConfig *container.HostConfig, networkConfig *network.NetworkingConfig, containerName string) *spec.Response {
	config := r.getContainerConfig(expModel)
	var defaultResponse *spec.Response
	command := r.CommandFunc(uid, ctx, expModel)
	sidecarContainerId, output, err, code := r.Client.executeAndRemove(
		config, hostConfig, networkConfig, containerName, true, time.Second, command)
	if err != nil {
		util.Errorf(uid, util.GetRunFuncName(), err.Error())
		return spec.ResponseFail(code, err.Error(), nil)
	}
	returnedResponse := ConvertContainerOutputToResponse(output, err, defaultResponse)
	logrus.Infof("sidecarContainerId for experiment %s is %s, output is %s, err is %v", uid, sidecarContainerId, output, err)
	return returnedResponse
}
