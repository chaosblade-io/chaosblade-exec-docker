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
	"path"
	"strconv"

	"github.com/sirupsen/logrus"

	"github.com/chaosblade-io/chaosblade-spec-go/spec"
)

// RunCmdInContainerExecutor is an executor interface which executes command in the target container directly
type RunCmdInContainerExecutor interface {
	spec.Executor
	DeployChaosBlade(ctx context.Context, containerId string, srcFile string, override bool) error
}

// RunCmdInContainerExecutorByCP is an executor implementation which used copy chaosblade tool to the target container and executed
type RunCmdInContainerExecutorByCP struct {
	BaseDockerClientExecutor
}

func NewRunCmdInContainerExecutorByCP() RunCmdInContainerExecutor {
	return &RunCmdInContainerExecutorByCP{
		BaseDockerClientExecutor{
			CommandFunc: commonFunc,
		},
	}
}

func (r *RunCmdInContainerExecutorByCP) Name() string {
	return "runCmdInContainerExecutorByCP"
}

func (r *RunCmdInContainerExecutorByCP) Exec(uid string, ctx context.Context, expModel *spec.ExpModel) *spec.Response {
	containerId := expModel.ActionFlags[ContainerIdFlag.Name]
	if containerId == "" {
		return spec.ReturnFail(spec.Code[spec.IllegalParameters], "less container id parameter")
	}
	if err := r.SetClient(expModel); err != nil {
		return spec.ReturnFail(spec.Code[spec.DockerInvokeError], err.Error())
	}
	command := r.CommandFunc(uid, ctx, expModel)
	if _, ok := spec.IsDestroy(ctx); !ok {
		// Create
		bladeTarFilePath := expModel.ActionFlags[ChaosBladeTarFilePathFlag.Name]
		overrideValue := expModel.ActionFlags[DeployBladeOverrideFlag.Name]
		override, err := strconv.ParseBool(overrideValue)
		if err != nil {
			override = false
		}
		err = r.DeployChaosBlade(ctx, containerId, bladeTarFilePath, override)
		if err != nil {
			return spec.ReturnFail(spec.Code[spec.DockerInvokeError], err.Error())
		}
	}
	output, err := r.Client.execContainer(containerId, command)
	var defaultResponse *spec.Response
	if err != nil {
		defaultResponse = spec.ReturnFail(spec.Code[spec.K8sInvokeError], err.Error())
	}
	return ConvertContainerOutputToResponse(output, err, defaultResponse)
}

func (r *RunCmdInContainerExecutorByCP) SetChannel(channel spec.Channel) {
}

func (r *RunCmdInContainerExecutorByCP) DeployChaosBlade(ctx context.Context, containerId string, srcFile string, override bool) error {
	// check if the blade tool exists
	output, err := r.Client.execContainer(containerId, fmt.Sprintf("ls %s", BladeBin))
	logrus.Debugf("output: %s, %v", output, err)
	if err == nil && !strings.Contains(output, "cannot access") && !override {
		return nil
	}
	err = r.Client.CopyToContainer(context.TODO(), containerId, srcFile, DstChaosBladeDir, override)
	if err != nil {
		return err
	}
	tarFileName := path.Base(srcFile)
	if index := strings.Index(tarFileName, ".t"); index > 0 {
		tarFileName = tarFileName[:index]
	}
	dstBladeDir := path.Join(DstChaosBladeDir, tarFileName)
	expectBladeDir := path.Join(DstChaosBladeDir, "chaosblade")
	renameCmd := fmt.Sprintf("rm -rf %s && mv %s %s", expectBladeDir, dstBladeDir, expectBladeDir)
	logrus.Debugf("renameCmd: %s", renameCmd)
	output, err = r.Client.execContainer(containerId, renameCmd)
	if err != nil {
		return err
	}
	if strings.TrimSpace(output) != "" {
		return fmt.Errorf(output)
	}
	return nil
}
