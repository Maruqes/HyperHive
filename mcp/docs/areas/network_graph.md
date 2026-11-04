# Network graph tool

Registration and safety: [shared policy and classifications](../tool-safety.md).

| Tool | Endpoint | Inputs | Result |
| --- | --- | --- | --- |
| `network_graph_get` | `GET /network/graph` | None | `{generatedAt, nodes, edges, warnings}` |

The graph is built on demand from configuration and is never stored. Each node has
`id`, `type`, `label`, `host`, `status` and `details`. Each edge has `source`, `target`,
`type` and `match`.

An edge is a configured route, not proof that traffic takes it. `match` records how a
target was identified: `dns_alias`, `hostname`, `ip`, `loopback`, `domain`, `upstream`,
or `none` for an `unknown` node. Structural edges have an empty `match`.

A failed source adds a string to `warnings` and leaves the graph partial, so a missing
node does not prove the resource is absent. New sources add node types without changing
this shape. Design and status: [network map](../../../docs/network-map.md).
