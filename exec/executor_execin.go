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
	"path"
	"strconv"
	"strings"

	"github.com/chaosblade-io/chaosblade-spec-go/util"
	"github.com/sirupsen/logrus"

	"github.com/chaosblade-io/chaosblade-spec-go/channel"
	"github.com/chaosblade-io/chaosblade-spec-go/spec"

	"github.com/chaosblade-io/chaosblade-exec-docker/version"
)

var defaultBladeTarFilePath = fmt.Sprintf("/opt/chaosblade-%s.tar.gz", version.BladeVersion)

// RunCmdInContainerExecutor is an executor interface which executes command in the target container directly
type RunCmdInContainerExecutor interface {
	spec.Executor
	DeployChaosBlade(ctx context.Context, containerId string, srcFile, extractDirName string, override bool) error
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
	if err := r.SetClient(expModel); err != nil {
		util.Errorf(uid, util.GetRunFuncName(), fmt.Sprintf(spec.ResponseErr[spec.DockerExecFailed].ErrInfo, "GetClient", err.Error()))
		return spec.ResponseFail(spec.DockerExecFailed, fmt.Sprintf(spec.ResponseErr[spec.DockerExecFailed].ErrInfo, "GetClient", err.Error()))
	}
	containerId := expModel.ActionFlags[ContainerIdFlag.Name]
	containerName := expModel.ActionFlags[ContainerNameFlag.Name]
	container, response := GetContainer(r.Client, uid, containerId, containerName)
	if !response.Success {
		return response
	}
	command := r.CommandFunc(uid, ctx, expModel)
	if _, ok := spec.IsDestroy(ctx); !ok {
		// Create
		bladeTarFilePath := expModel.ActionFlags[ChaosBladeTarFilePathFlag.Name]
		if bladeTarFilePath == "" {
			bladeTarFilePath = defaultBladeTarFilePath
		}
		overrideValue := expModel.ActionFlags[DeployBladeOverrideFlag.Name]
		override, err := strconv.ParseBool(overrideValue)
		if err != nil {
			override = false
		}
		if resp, ok := channel.NewLocalChannel().IsAllCommandsAvailable([]string{"tar"}); !ok {
			util.Errorf(uid, util.GetRunFuncName(), resp.Err)
			return resp
		}

		response := channel.NewLocalChannel().Run(context.Background(), "tar",
			fmt.Sprintf("tf %s| head -1 | cut -f1 -d/", bladeTarFilePath))
		if !response.Success {
			util.Errorf(uid, util.GetRunFuncName(), fmt.Sprintf("`%s`: blade-tar-file parameter is invalid, err: %s", bladeTarFilePath, response.Err))
			return spec.ResponseFail(spec.ParameterInvalid, fmt.Sprintf(spec.ResponseErr[spec.ParameterInvalid].Err, ChaosBladeTarFilePathFlag.Name))
		}
		if response.Result == nil {
			util.Errorf(uid, util.GetRunFuncName(), fmt.Sprintf("`%s`: blade-tar-file parameter is invalid, extract directory failed", bladeTarFilePath))
			return spec.ResponseFail(spec.ParameterInvalid, fmt.Sprintf(spec.ResponseErr[spec.ParameterInvalid].Err, ChaosBladeTarFilePathFlag.Name))
		}
		extractedDirName := strings.TrimSpace(response.Result.(string))
		if extractedDirName == "" {
			util.Errorf(uid, util.GetRunFuncName(), fmt.Sprintf("`%s`: blade-tar-file parameter is invalid, extract empty directory failed", bladeTarFilePath))
			return spec.ResponseFail(spec.ParameterInvalid, fmt.Sprintf(spec.ResponseErr[spec.ParameterInvalid].Err, ChaosBladeTarFilePathFlag.Name))
		}
		err = r.DeployChaosBlade(ctx, container.ID, bladeTarFilePath, extractedDirName, override)
		if err != nil {
			util.Errorf(uid, util.GetRunFuncName(), fmt.Sprintf(spec.ResponseErr[spec.DockerExecFailed].ErrInfo, "DeployChaosBlade", err.Error()))
			return spec.ResponseFail(spec.DockerExecFailed, fmt.Sprintf(spec.ResponseErr[spec.DockerExecFailed].ErrInfo, "DeployChaosBlade", err.Error()))
		}
	}
	output, err := r.Client.execContainer(container.ID, command)
	var defaultResponse *spec.Response
	if err != nil {
		util.Errorf(uid, util.GetRunFuncName(), fmt.Sprintf(spec.ResponseErr[spec.DockerExecFailed].ErrInfo, "execContainer ", err.Error()))
		return spec.ResponseFail(spec.DockerExecFailed, fmt.Sprintf(spec.ResponseErr[spec.DockerExecFailed].ErrInfo, "execContainer", err.Error()))
	}
	return ConvertContainerOutputToResponse(output, err, defaultResponse)
}

func (r *RunCmdInContainerExecutorByCP) SetChannel(channel spec.Channel) {
}

func (r *RunCmdInContainerExecutorByCP) DeployChaosBlade(ctx context.Context, containerId string,
	srcFile, extractDirName string, override bool) error {
	// check if the blade tool exists
	output, err := r.Client.execContainerPrivileged(containerId, fmt.Sprintf("[ -e %s ] && echo True || echo False", BladeBin))
	logrus.Debugf("output: %s, %v", output, err)
	if err == nil && strings.Contains(output, "True") && !override {
		return nil
	}
	err = r.Client.CopyToContainer(context.TODO(), containerId, srcFile, DstChaosBladeDir, override)
	if err != nil {
		return err
	}
	dstBladeDir := path.Join(DstChaosBladeDir, extractDirName)
	expectBladeDir := path.Join(DstChaosBladeDir, "chaosblade")
	renameCmd := fmt.Sprintf("rm -rf %s && mv %s %s", expectBladeDir, dstBladeDir, expectBladeDir)
	logrus.Debugf("renameCmd: %s", renameCmd)
	_, err = r.Client.execContainerPrivileged(containerId, renameCmd)
	return err
}
