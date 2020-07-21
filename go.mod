module github.com/chaosblade-io/chaosblade-exec-docker

go 1.13

require (
	github.com/Microsoft/go-winio v0.4.14 // indirect
	github.com/chaosblade-io/chaosblade-exec-os v0.6.0
	github.com/chaosblade-io/chaosblade-spec-go v0.6.1-0.20200628025133-fa9dc1fa51a6
	github.com/docker/distribution v2.7.1+incompatible // indirect
	github.com/docker/docker v0.0.0-20180612054059-a9fbbdc8dd87
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/opencontainers/go-digest v1.0.0-rc1 // indirect
	github.com/opencontainers/image-spec v1.0.1 // indirect
	github.com/sirupsen/logrus v1.5.0
	golang.org/x/net v0.0.0-20191204025024-5ee1b9f4859a // indirect
)

replace (
	github.com/chaosblade-io/chaosblade-exec-os v0.6.0 => /Users/Shared/ChaosBladeProjects/chaosblade-opensource/chaosblade-exec-os
	github.com/chaosblade-io/chaosblade-spec-go v0.6.1-0.20200628025133-fa9dc1fa51a6 => /Users/Shared/ChaosBladeProjects/chaosblade-opensource/chaosblade-spec-go
)
