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
	if suid, ok := spec.IsDestroy(ctx); ok {
		if suid == spec.UnknownUid {
			matchers := spec.ConvertExpMatchersToString(model, func() map[string]spec.Empty {
				return GetAllDockerFlagNames()
			})
			return fmt.Sprintf("%s destroy %s %s %s", BladeBin, model.Target, model.ActionName, matchers)
		}
		return fmt.Sprintf("%s destroy %s", BladeBin, suid)
	} else {
		matchers := spec.ConvertExpMatchersToString(model, func() map[string]spec.Empty {
			return GetAllDockerFlagNames()
		})
		return fmt.Sprintf("%s create %s %s %s --uid %s", BladeBin, model.Target, model.ActionName, matchers, uid)
	}
}

func ConvertContainerOutputToResponse(output string, err error, defaultResponse *spec.Response) *spec.Response {
	if err != nil {
		response := spec.Decode(err.Error(), defaultResponse)
		if response.Success {
			return response
		}
		return spec.ReturnFail(spec.Code[spec.DockerInvokeError], err.Error())
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return spec.ReturnFail(spec.Code[spec.DockerInvokeError], "cannot get output message")
	}
	response := spec.Decode(output, defaultResponse)
	if response.Success {
		return response
	}
	if response.Code == spec.Code[spec.DecodeError].Code {
		return spec.ReturnFail(spec.Code[spec.DockerInvokeError], output)
	}
	return response
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
