# ARO-HCP

# Description
The RP for the ARO-HCP project.


## Development setup

The setup is based on VSCode Remote Containers. See [here](https://code.visualstudio.com/docs/remote/containers) for more information.

The predefined container is in `.devcontainer` with a custom `postCreate.sh`.
To use it, please install the [Remote - Containers](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-containers) extension in VSCode.

The VSCode will have the following extensions installed:
- [golang.go](https://marketplace.visualstudio.com/items?itemName=golang.Go)
- [editorconfig.editorconfig](https://marketplace.visualstudio.com/items?itemName=EditorConfig.EditorConfig)
- [ms-azuretools.vscode-bicep](https://marketplace.visualstudio.com/items?itemName=ms-azuretools.vscode-bicep)
- [ms-vscode.azurecli](https://marketplace.visualstudio.com/items?itemName=ms-vscode.azurecli)
- [arjun.swagger-viewer](https://marketplace.visualstudio.com/items?itemName=Arjun.swagger-viewer)

During the container setup, it also install golangci-lint, which is defacto standard for linting go.

On top of that, is sets up the Bicep CLI and the Azure CLI with Bicep extension
to simplify development of infra code.

Finally, the container also contains the nodejs and sets up the typespec which is needed for the ARM contract development, as it is now mandatory to have the typespec in the ARM templates.
To enable the typespec extensions, which is not yet part of official extensions, once the vscode opens and the devcontainer is ready, you need to run the following command
```bash
tsp code install
```

If you are developing on MacOS you will need to install both docker cli (NOT docker desktop) and colima. There have been issues with devcontainer working with vscode using podman desktop.

```bash
brew install docker
brew install colima
```

Before running your devcontainer, make sure colima is started.
```bash
colima start
```

Then, rebuild and connect to the dev container: `cmd + shift + P` => `dev containers: rebuild container`

**Most importantly**, the container is set up to use the same user as the host machine, so you can use the same git config and ssh keys.
It is implemented as host mount in the `.devcontainer/devcontainer.json` file.

```json
"mounts": [
    "source=${localEnv:HOME}/.gitconfig,target=/home/vscode/.gitconfig,type=bind,consistency=cached"
],
```