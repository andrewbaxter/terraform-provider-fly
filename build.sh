#!/usr/bin/env bash -xeu
go run -modfile tools.mod github.com/Khan/genqlient graphql/genqlient.yaml
go build
rm -rf docs
mkdir docs
go run -modfile tools.mod github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name fly --rendered-provider-name "Fly.io" --rendered-website-dir docs
