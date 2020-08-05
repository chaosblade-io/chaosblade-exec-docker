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
	"fmt"

	"github.com/chaosblade-io/chaosblade-exec-os/exec"
	"github.com/chaosblade-io/chaosblade-spec-go/spec"
)

type ResourceExpModelSpec interface {
	// Scope
	Scope() string
	// ExpModels returns the map of the experiment name and the model
	ExpModels() map[string]spec.ExpModelCommandSpec
	// GetExpActionModelSpec returns the action spec
	GetExpActionModelSpec(target, action string) spec.ExpActionCommandSpec
}

func NewDockerExpModelSpec() *dockerExpModelSpec {
	modelSpec := &dockerExpModelSpec{
		ScopeName:     "docker",
		ExpModelSpecs: make(map[string]spec.ExpModelCommandSpec, 0),
	}
	networkCommandModelSpec := exec.NewNetworkCommandSpec()
	execSidecarModelSpecs := []spec.ExpModelCommandSpec{
		networkCommandModelSpec,
	}
	execInContainerModelSpecs := []spec.ExpModelCommandSpec{
		exec.NewProcessCommandModelSpec(),
		exec.NewCpuCommandModelSpec(),
		exec.NewDiskCommandSpec(),
		exec.NewMemCommandModelSpec(),
		exec.NewFileCommandSpec(),
	}
	containerSelfModelSpec := NewContainerCommandSpec()

	spec.AddExecutorToModelSpec(NewNetWorkSidecarExecutor(), networkCommandModelSpec)
	spec.AddExecutorToModelSpec(NewRunCmdInContainerExecutorByCP(), execInContainerModelSpecs...)
	spec.AddFlagsToModelSpec(GetExecSidecarFlags, execSidecarModelSpecs...)
	spec.AddFlagsToModelSpec(GetContainerSelfFlags, containerSelfModelSpec)
	spec.AddFlagsToModelSpec(GetExecInContainerFlags, execInContainerModelSpecs...)

	expModelCommandSpecs := append(execSidecarModelSpecs, execInContainerModelSpecs...)
	expModelCommandSpecs = append(expModelCommandSpecs, containerSelfModelSpec)
	modelSpec.addExpModels(expModelCommandSpecs...)
	return modelSpec
}

type dockerExpModelSpec struct {
	ScopeName     string
	ExpModelSpecs map[string]spec.ExpModelCommandSpec
}

func (b *dockerExpModelSpec) Scope() string {
	return b.ScopeName
}

func (b *dockerExpModelSpec) ExpModels() map[string]spec.ExpModelCommandSpec {
	return b.ExpModelSpecs
}

func (b *dockerExpModelSpec) GetExpActionModelSpec(target, actionName string) spec.ExpActionCommandSpec {
	commandSpec := b.ExpModelSpecs[target]
	if commandSpec == nil {
		return nil
	}
	actions := commandSpec.Actions()
	if actions == nil {
		return nil
	}
	for _, action := range actions {
		if action.Name() == actionName {
			return action
		}
		for _, alias := range action.Aliases() {
			if alias == actionName {
				return action
			}
		}
	}
	return nil
}

func (b *dockerExpModelSpec) addExpModels(expModel ...spec.ExpModelCommandSpec) {
	for _, model := range expModel {
		b.ExpModelSpecs[model.Name()] = model
	}
}

func GetAllExecutors() map[string]spec.Executor {
	executors := make(map[string]spec.Executor, 0)
	dockerModelSpecs := NewDockerExpModelSpec()
	for _, expModel := range dockerModelSpecs.ExpModels() {
		executorMap := extractExecutorFromExpModel(expModel)
		for key, value := range executorMap {
			executors[key] = value
		}
	}
	return executors
}

func extractExecutorFromExpModel(expModel spec.ExpModelCommandSpec) map[string]spec.Executor {
	executors := make(map[string]spec.Executor)
	for _, actionModel := range expModel.Actions() {
		executors[GetExecutorKey(expModel.Name(), actionModel.Name())] = actionModel.Executor()
	}
	return executors
}

var ContainerIdFlag = &spec.ExpFlag{
	Name:                  "container-id",
	Desc:                  "Container id",
	NoArgs:                false,
	Required:              false,
	RequiredWhenDestroyed: true,
}

var ImageRepoFlag = &spec.ExpFlag{
	Name:     "image-repo",
	Desc:     "Image repository of the chaosblade-tool",
	NoArgs:   false,
	Required: false,
}

var ImageVersionFlag = &spec.ExpFlag{
	Name:     "image-version",
	Desc:     "Image version of the chaosblade-tool",
	NoArgs:   false,
	Required: false,
}

var EndpointFlag = &spec.ExpFlag{
	Name:     "docker-endpoint",
	Desc:     "Docker socket endpoint",
	NoArgs:   false,
	Required: false,
}

var ChaosBladeTarFilePathFlag = &spec.ExpFlag{
	Name:     "blade-tar-file",
	Desc:     "The pull path of the ChaosBlade tar package, for example, --blade-tar-file /opt/chaosblade-0.4.0.tar.gz",
	NoArgs:   false,
	Required: false,
}

var DeployBladeOverrideFlag = &spec.ExpFlag{
	Name:     "blade-override",
	Desc:     "Override the exists chaosblade tool in the target container or not, default value is false",
	NoArgs:   true,
	Required: false,
}

func GetContainerSelfFlags() []spec.ExpFlagSpec {
	return []spec.ExpFlagSpec{
		ContainerIdFlag,
		EndpointFlag,
	}
}

func GetExecSidecarFlags() []spec.ExpFlagSpec {
	return []spec.ExpFlagSpec{
		ContainerIdFlag,
		ImageRepoFlag,
		ImageVersionFlag,
		EndpointFlag,
	}
}

func GetExecInContainerFlags() []spec.ExpFlagSpec {
	return []spec.ExpFlagSpec{
		ContainerIdFlag,
		ImageRepoFlag,
		ImageVersionFlag,
		EndpointFlag,
		ChaosBladeTarFilePathFlag,
		DeployBladeOverrideFlag,
	}
}

func GetAllDockerFlagNames() map[string]spec.Empty {
	flagNames := make(map[string]spec.Empty, 0)
	for _, flag := range GetExecInContainerFlags() {
		flagNames[flag.FlagName()] = spec.Empty{}
	}
	return flagNames
}

func GetExecutorKey(target, action string) string {
	return fmt.Sprintf("%s-%s", target, action)
}
