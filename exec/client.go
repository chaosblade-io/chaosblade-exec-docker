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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/chaosblade-io/chaosblade-spec-go/spec"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/versions"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
)

const (
	ChaosBladeImageVersion = "latest"
	DefaultImageRepo       = "registry.cn-hangzhou.aliyuncs.com/chaosblade/chaosblade-tool"
)

var cli *Client

type Client struct {
	client *client.Client
}

//GetClient returns the docker client
func GetClient(endpoint string) (*Client, error) {
	var oldClient *client.Client
	if cli != nil {
		oldClient = cli.client
	}
	client, err := checkAndCreateClient(endpoint, oldClient)
	if err != nil {
		return nil, err
	}
	cli = &Client{client: client}
	return cli, nil
}

// CopyToContainer copies a tar file to the dstPath.
// If the same file exits in the dstPath, it will be override if the override arg is true, otherwise not
func (c *Client) CopyToContainer(ctx context.Context, containerId, srcFile, dstPath string, override bool) error {
	// must be a tar file
	options := types.CopyToContainerOptions{
		AllowOverwriteDirWithFile: override,
		CopyUIDGID:                true,
	}
	_, err := c.execContainerPrivileged(containerId, fmt.Sprintf("mkdir -p %s", dstPath))
	if err != nil {
		return err
	}
	file, err := os.OpenFile(srcFile, os.O_RDONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	return c.client.CopyToContainer(ctx, containerId, dstPath, file, options)
}

// getContainerById returns the container object by container id
func (c *Client) getContainerById(containerId string) (types.Container, error, int32) {
	containers, err := c.client.ContainerList(context.Background(), types.ContainerListOptions{
		Filters: filters.NewArgs(
			filters.Arg("id", containerId),
		),
	})
	if err != nil {
		return types.Container{}, fmt.Errorf(spec.DockerExecFailed.Sprintf("GetContainerList")), spec.DockerExecFailed.Code
	}
	if containers == nil || len(containers) == 0 {
		return types.Container{}, fmt.Errorf(spec.ParameterInvalidDockContainerId.Sprintf("container-id")), spec.ParameterInvalidDockContainerId.Code
	}
	return containers[0], nil, spec.OK.Code
}

//getContainerByName returns the container object by container name
func (c *Client) getContainerByName(containerName string) (types.Container, error, int32) {
	containers, err := c.client.ContainerList(context.Background(), types.ContainerListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("name", containerName),
		),
	})
	if err != nil {
		return types.Container{}, fmt.Errorf(spec.DockerExecFailed.Sprintf("GetContainerList", err)), spec.DockerExecFailed.Code
	}
	if containers == nil || len(containers) == 0 {
		return types.Container{}, fmt.Errorf(spec.ParameterInvalidDockContainerName.Sprintf("container-name")), spec.ParameterInvalidDockContainerName.Code
	}
	return containers[0], nil, spec.OK.Code
}

//ExecuteAndRemove: create and start a container for executing a command, and remove the container
func (c *Client) executeAndRemove(config *container.Config, hostConfig *container.HostConfig,
	networkConfig *network.NetworkingConfig, containerName string, removed bool, timeout time.Duration,
	command string) (containerId string, output string, err error, code int32) {

	logrus.Debugf("command: '%s', image: %s, containerName: %s", command, config.Image, containerName)
	// check image exists or not
	_, err = c.getImageByRef(config.Image)
	if err != nil {
		// pull image if not exists
		_, err := c.pullImage(config.Image)
		if err != nil {
			return "", "", fmt.Errorf(spec.DockerImagePullFailed.Sprintf(err)), spec.DockerImagePullFailed.Code
		}
	}
	containerId, err = c.createAndStartContainer(config, hostConfig, networkConfig, containerName)
	if err != nil {
		c.stopAndRemoveContainer(containerId, &timeout)
		return containerId, "", fmt.Errorf(spec.DockerExecFailed.Sprintf("CreateAndStartContainer", err)), spec.DockerExecFailed.Code
	}

	output, err = c.execContainer(containerId, command)
	if err != nil {
		if removed {
			c.stopAndRemoveContainer(containerId, &timeout)
		}
		return containerId, "", fmt.Errorf(spec.DockerExecFailed.Sprintf("ContainerExecCmd", err)), spec.DockerExecFailed.Code
	}
	logrus.Infof("Execute output in container: %s", output)
	if removed {
		c.stopAndRemoveContainer(containerId, &timeout)
	}
	return containerId, output, nil, spec.OK.Code
}

// waitAndGetOutput returns the result
func (c *Client) waitAndGetOutput(containerId string) (string, error) {
	containerWait()
	resp, err := c.client.ContainerLogs(context.Background(), containerId, types.ContainerLogsOptions{
		ShowStderr: true,
		ShowStdout: true,
	})
	if err != nil {
		logrus.Warningf("Get container: %s log err: %s", containerId, err)
		return "", err
	}
	defer resp.Close()
	bytes, err := ioutil.ReadAll(resp)
	return string(bytes), err
}

func containerWait() error {
	timer := time.NewTimer(500 * time.Millisecond)
	select {
	case <-timer.C:
	}
	return nil
}

//createAndStartContainer
func (c *Client) createAndStartContainer(config *container.Config, hostConfig *container.HostConfig,
	networkConfig *network.NetworkingConfig, containerName string) (string, error) {
	body, err := c.client.ContainerCreate(context.Background(), config, hostConfig, networkConfig, containerName)
	if err != nil {
		logrus.Warningf("Create container: %s, err: %s", containerName, err.Error())
		return "", err
	}
	containerId := body.ID
	err = c.startContainer(containerId)
	return containerId, err
}

//startContainer
func (c *Client) startContainer(containerId string) error {
	err := c.client.ContainerStart(context.Background(), containerId, types.ContainerStartOptions{})
	if err != nil {
		logrus.Warningf("Start container: %s, err: %s", containerId, err.Error())
		return err
	}
	return nil
}

func (c *Client) execContainer(containerId, command string) (output string, err error) {
	return c.execContainerWithConf(containerId, command, types.ExecConfig{
		AttachStderr: true,
		AttachStdout: true,
		Cmd:          []string{"sh", "-c", command},
	})
}

func (c *Client) execContainerPrivileged(containerId, command string) (output string, err error) {
	return c.execContainerWithConf(containerId, command, types.ExecConfig{
		AttachStderr: true,
		AttachStdout: true,
		Cmd:          []string{"sh", "-c", command},
		Privileged:   true,
		User:         "root",
	})
}

//execContainer with command which does not contain "sh -c" in the target container
func (c *Client) execContainerWithConf(containerId, command string, config types.ExecConfig) (output string, err error) {
	logrus.Infof("execute command: %s", strings.Join(config.Cmd, " "))
	ctx := context.Background()
	id, err := c.client.ContainerExecCreate(ctx, containerId, config)
	if err != nil {
		logrus.Warningf("Create exec for container: %s, err: %s", containerId, err.Error())
		return "", err
	}
	resp, err := c.client.ContainerExecAttach(ctx, id.ID, types.ExecStartCheck{})
	if err != nil {
		logrus.Warningf("Attach exec for container: %s, err: %s", containerId, err.Error())
		return "", err
	}
	defer resp.Close()
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	_, err = stdcopy.StdCopy(stdout, stderr, resp.Reader)
	if err != nil {
		logrus.Warningf("Attach exec for container: %s, err: %s", containerId, err.Error())
		return "", err
	}
	result := stdout.String()
	errorMsg := stderr.String()
	logrus.Debugf("execute result: %s, error msg: %s", result, errorMsg)
	if errorMsg != "" {
		return "", fmt.Errorf(errorMsg)
	} else {
		return result, nil
	}
}

//StopContainer
func (c *Client) stopContainer(containerId string, timeout *time.Duration) error {
	ctx := context.Background()
	err := c.client.ContainerStop(ctx, containerId, nil)
	if err != nil {
		logrus.Warningf("Stop container: %s, err: %s", containerId, err)
		return err
	}
	return nil
}

//StopAndRemoveContainer
func (c *Client) forceRemoveContainer(containerId string) error {
	err := c.client.ContainerRemove(context.Background(), containerId, types.ContainerRemoveOptions{
		Force: true,
	})
	if err != nil {
		logrus.Warningf("Remove container: %s, err: %s", containerId, err)
		return err
	}
	return nil
}

//StopAndRemoveContainer
func (c *Client) stopAndRemoveContainer(containerId string, timeout *time.Duration) error {
	if err := c.stopContainer(containerId, timeout); err != nil {
		_, err, code := c.getContainerById(containerId)
		if err != nil && (code == spec.ParameterInvalidDockContainerId.Code) {
			return nil
		}
		return err
	}
	err := c.client.ContainerRemove(context.Background(), containerId, types.ContainerRemoveOptions{
		Force: true,
	})
	if err != nil {
		logrus.Warningf("Remove container: %s, err: %s", containerId, err)
		return err
	}
	return nil
}

//GetImageInspectById
func (c *Client) getImageInspectById(imageId string) (types.ImageInspect, error) {
	inspect, _, err := c.client.ImageInspectWithRaw(context.Background(), imageId)
	return inspect, err
}

//ImageExists
func (c *Client) getImageByRef(ref string) (types.ImageSummary, error) {
	args := filters.NewArgs(filters.Arg("reference", ref))
	list, err := c.client.ImageList(context.Background(), types.ImageListOptions{
		All:     false,
		Filters: args,
	})
	if err != nil {
		logrus.Warningf("Get image by name failed. name: %s, err: %s", ref, err)
		return types.ImageSummary{}, err
	}
	if len(list) == 0 {
		logrus.Warningf("Cannot find the image by name: %s", ref)
		return types.ImageSummary{}, errors.New("image not found")
	}
	return list[0], nil
}

//DeleteImageByImageId
func (c *Client) deleteImageByImageId(imageId string) error {
	_, err := c.client.ImageRemove(context.Background(), imageId, types.ImageRemoveOptions{
		Force:         false,
		PruneChildren: true,
	})
	return err
}

//PullImage
func (c *Client) pullImage(ref string) (string, error) {
	reader, err := c.client.ImagePull(context.Background(), ref, types.ImagePullOptions{})
	if err != nil {
		return "", err
	}
	defer reader.Close()
	bytes, err := ioutil.ReadAll(reader)
	return string(bytes), nil
}

//checkAndCreateClient
func checkAndCreateClient(endpoint string, cli *client.Client) (*client.Client, error) {
	if cli == nil {
		var err error
		if endpoint == "" {
			cli, err = client.NewClientWithOpts(client.FromEnv, client.WithVersion("1.24"))
		} else {
			cli, err = client.NewClientWithOpts(client.FromEnv, client.WithVersion("1.24"), client.WithHost(endpoint))
		}
		if err != nil {
			return nil, err
		}
	}
	return ping(cli)
}

// ping
func ping(cli *client.Client) (*client.Client, error) {
	if cli == nil {
		return nil, errors.New("client is nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p, err := cli.Ping(ctx)
	if err == nil {
		return cli, nil
	}
	if p.APIVersion == "" {
		return nil, err
	}
	// if server version is lower than the client version, downgrade
	if versions.LessThan(p.APIVersion, cli.ClientVersion()) {
		client.WithVersion(p.APIVersion)(cli)
		_, err = cli.Ping(ctx)
		if err == nil {
			return cli, nil
		}
		return nil, err
	}
	return nil, err
}

func getChaosBladeImageRef(repo, version string) string {
	if repo == "" {
		repo = DefaultImageRepo
	}
	if version == "" {
		version = ChaosBladeImageVersion
	}
	return fmt.Sprintf("%s:%s", repo, version)
}
