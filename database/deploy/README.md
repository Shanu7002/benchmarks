# Deploy (future)

Placeholder for Terraform / CloudFormation that provisions a target DB and a
runner host in the same region, then runs `dbbench` against it. The binary is
static (`CGO_ENABLED=0 go build ./cmd/dbbench`), so it can be copied to a VM
or baked into a container image. Keep the instance size in each target's
`label` so results from different environments stay comparable.
