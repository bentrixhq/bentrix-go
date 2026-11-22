# Bentrix SDK for Go

The Go client of [Bentrix](https://bentrix.net) — charts, a replay laboratory and trading through your own MetaTrader — for a program or an agent.

It finds the Bentrix running on this computer through the agents folder Bentrix writes at every start, pairs with it under the name you give — the name you see in ☰ › Settings › **Agents**, where the pairing's permissions are set — and calls every tool of Bentrix's capability catalogue over MCP. Nothing is typed: no address, no key.

The client is generated from the catalogue of the Bentrix release it came with, and checks when it connects that the running Bentrix serves the same major version.

## What you need

Go 1.23 or later, and Bentrix open on the same computer.

## Install

The module is `bentrix`, package `bentrix/mcp`. Clone this repository beside your own module and point to it:

```bash
go mod init myagent
go mod edit -require bentrix@v0.0.0 -replace bentrix=../bentrix-go
```

## Connect

```go
ctx := context.Background()
b, err := mcp.Connect(ctx, "my-agent")
if err != nil {
	log.Fatal(err)
}
state, err := b.Workspace.GetState(ctx, mcp.WorkspaceGetStateArgs{})
```

Every tool is a method taking a context and one arguments struct named after the tool; an optional field is a pointer, set with `mcp.Ptr(value)`. A refusal is a `*mcp.ToolError` carrying Bentrix's own sentence.

## The engine in your own process

The `bentrix` package and its Gym (the in-process engine) use Bentrix's engine library, which is not part of this repository.

## Documentation

Every method, its arguments, the permission it needs and what it answers: **https://bentrix.net/en/docs/sdk/go/**

## Licence

MIT — see [LICENSE](LICENSE). It covers this source code only. Bentrix's engine library that some of these bindings load (`libbentrix`), and the Bentrix application, are Bentrix's own and distributed under their own terms.
