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
	"strings"

	"github.com/chaosblade-io/chaosblade-spec-go/spec"
	"github.com/chaosblade-io/chaosblade-spec-go/util"
	"github.com/docker/docker/api/types"
)

const DstChaosBladeDir = "/opt"

// BladeBin is the blade path in the chaosblade-tool image
const BladeBin = "/opt/chaosblade/blade"

// BaseDockerClientExecutor
type BaseDockerClientExecutor struct {
	Client      *Client
	CommandFunc func(uid string, ctx context.Context, model *spec.ExpModel) string
}

// commonFunc is the command created function
var commonFunc = func(uid string, ctx context.Context, model *spec.ExpModel) string {
	matchers := spec.ConvertExpMatchersToString(model, func() map[string]spec.Empty {
		return GetAllDockerFlagNames()
	})
	if _, ok := spec.IsDestroy(ctx); ok {
		// UPDATE: https://github.com/chaosblade-io/chaosblade/issues/334
		return fmt.Sprintf("%s destroy %s %s %s", BladeBin, model.Target, model.ActionName, matchers)
	}
	return fmt.Sprintf("%s create %s %s %s --uid %s", BladeBin, model.Target, model.ActionName, matchers, uid)
}

func ConvertContainerOutputToResponse(output string, err error, defaultResponse *spec.Response) *spec.Response {
	if err != nil {
		response := spec.Decode(err.Error(), defaultResponse)
		if response.Success {
			return response
		}
		return spec.ResponseFailWithFlags(spec.DockerExecFailed, "execContainer", err)
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return spec.ResponseFailWithFlags(spec.DockerExecFailed, "execContainer",
			"cannot get result message from docker container, please execute recovery and try again")
	}
	return spec.Decode(output, defaultResponse)
}

// SetClient to the executor
func (b *BaseDockerClientExecutor) SetClient(expModel *spec.ExpModel) error {
	cli, err := GetClient(expModel.ActionFlags[EndpointFlag.Name])
	if err != nil {
		return err
	}
	b.Client = cli
	return nil
}

// GetContainer return container by container flag, such as container id or container name.
func GetContainer(client *Client, uid string, containerId, containerName string) (types.Container, *spec.Response) {
	if containerId == "" && containerName == "" {
		tips := fmt.Sprintf("%s or %s", ContainerIdFlag.Name, ContainerNameFlag.Name)
		util.Errorf(uid, util.GetRunFuncName(), spec.ParameterLess.Sprintf(tips))
		return types.Container{}, spec.ResponseFailWithFlags(spec.ParameterLess, tips)
	}
	var container types.Container
	var code int32
	var err error
	if containerId != "" {
		container, err, code = client.getContainerById(containerId)
	} else {
		container, err, code = client.getContainerByName(containerName)
	}
	if err != nil {
		util.Errorf(uid, util.GetRunFuncName(), err.Error())
		return container, spec.ResponseFail(code, err.Error(), nil)
	}
	return container, spec.ReturnSuccess(container)
}
