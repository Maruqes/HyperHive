# celld observability — contrato para frontend

Contrato do backend local de 2026-10-18; ainda sem deploy. Exemplos abaixo são
ilustrativos, com valores fictícios. Não é necessário integrar gRPC, OTLP ou storage.

## Acesso e endpoints

Usar a mesma base URL e autenticação da API HyperHive existente:
`Authorization: Bearer <token>`. O prefixo `/api` também é aceite.
Todas as respostas têm `Cache-Control: no-store`.
O ID vem de `GET /celld/fleets` → `fleets[].id`.

| GET | Conteúdo |
| --- | --- |
| `/celld/fleets/{id}/observability` | Estado dos nós, recursos, workloads, capacidades e utilização do histórico |
| `/celld/fleets/{id}/logs` | Logs paginados e filtráveis |
| `/celld/fleets/{id}/traces/{trace_id}` | Spans e logs com esse trace ID, dentro da fleet |
| `/celld/fleets/{id}/metrics` | Histórico de recursos + resumo de pedidos observados |
| `/celld/fleets/{id}/events` | Eventos de arranque/saída dos processos |

## Overview

```json
{
  "fleet_id": "f123",
  "historical": false,
  "nodes": [{
    "fleet_id": "f123",
    "node_id": "127.0.0.2",
    "version": "v0.6.2",
    "process_generation": "0123456789abcdef0123456789abcdef",
    "collected_ms": 1792320000000,
    "attempted_ms": 1792320000000,
    "state": "fresh",
    "resources": {
      "sampled_ms": 1792320000000,
      "owned_cells": 10,
      "resident_cells": 4,
      "rss_bytes": 52428800,
      "cpu_percent_x100": 1250,
      "pressured": false,
      "memory_headroom": true,
      "restoring": 0
    },
    "workloads": [{
      "kind": "worker", "id": "app", "version": "abc123",
      "state": "current", "live": 1, "retiring": 0,
      "heap_bytes": 2097152, "cells": 0, "requests": 0
    }],
    "capabilities": [{
      "name": "queue_counters", "state": "unsupported",
      "detail": "No operator backlog/oldest/in-flight/retry/dead-letter API"
    }]
  }],
  "deployment": {
    "collected_ms": 1792320000000,
    "state": "fresh",
    "workloads": [{"kind": "queue", "id": "JOBS", "parent": "app", "state": "configured"}]
  },
  "storage": {
    "bytes": 10485760,
    "global_bytes": 20971520,
    "fleet_budget_bytes": 1073741824,
    "global_budget_bytes": 10737418240,
    "retention_days": 7,
    "dropped": 0,
    "rejected_batches": 0,
    "pruned_segments": 0
  },
  "cpu_aggregation": "per-node process percentages; not averaged; 100% equals one CPU core",
  "incomplete": true
}
```

`nodes[].node_id` é o IP de gestão, usado no filtro `node`.
`nodes[].workloads` são observações do runtime; `deployment.workloads` são
configuração/bindings, não contagens de execução. A versão de um pool refere-se
à geração/deployment observada; linhas de células podem usar `generation:N`.
`workloads[].parent` dá a relação quando disponível; os tipos devem aceitar
strings novas. Exemplos: `worker`, `cell_pool`, `cell`, `queue_cell`,
`workflow_cell`, `d1_cell`, `kv_cell`, `cron_cell`, `container`, `python`,
`queue`, `workflow`, `cron`, `container_class` e bindings.

Recursos também podem incluir `in_use_bytes`, `open_fds`, `fd_limit`,
`host_websockets`, `shed_cells`, `capacity_waiting`, `activation_waiting`,
`handoff_failed`, `phases` e `container_instances`. Workloads podem incluir
`memory_bytes`, `cpu_percent` e `limit`.

## Logs, eventos e traces: envelope comum

```json
{
  "records": [{
    "id": "identificador-opaco-estavel",
    "node": "127.0.0.2",
    "generation": "0123456789abcdef0123456789abcdef",
    "received_ms": 1792320005000,
    "record": {
      "sequence": 42,
      "time_ms": 1792320000000,
      "kind": "log",
      "source": "otlp",
      "severity": "ERROR",
      "body": "Falha ao processar pedido",
      "trace_id": "0123456789abcdef0123456789abcdef",
      "span_id": "0123456789abcdef"
    }
  }],
  "next_cursor": "cursor-opaco",
  "from": 1792316400000,
  "to": 1792320000000,
  "truncated": true,
  "incomplete": true
}
```

- A lista fica em `records`; o conteúdo fica em `records[i].record`.
- `id` é a chave para deduplicação. Não interpretar o seu formato.
- `record.kind`: `log`, `event` ou `span`. Um trace pode conter logs e spans.
- Fontes: `otlp`, `stdout`, `stderr`, `container`, `lifecycle`.
- Spans podem incluir `parent_span_id`, `duration_us`, `failed` e `attributes`.
- Logs podem não ter `trace_id`, `span_id` ou `workload`.
- Logs de containers identificam o stream em `attributes.stream`.
- `record.time_ms` é a data do evento; `received_ms` é a receção central.
- A ordem é cronológica ascendente, com desempate por `id`.
- Sem resultados: `records: []`. Não confundir com `error` preenchido.

## Filtros e paginação

| Parâmetro | Regra |
| --- | --- |
| `from`, `to` | Unix **milissegundos**; por omissão última hora; máximo 7 dias |
| `limit` | 1–500; por omissão 200 |
| `cursor` | Opaco; continuar com `next_cursor` e manter os mesmos filtros/intervalo |
| `node` | IP de gestão; igualdade exata |
| `workload` | Identificador observado; igualdade exata; pode não existir em todos os logs |
| `source` | Fonte exata; `process` agrupa `stdout` e `stderr` |
| `severity` | Igualdade exata, por exemplo `ERROR`; não significa “ERROR ou superior” |
| `text` | Substring literal, sensível a maiúsculas/minúsculas; máximo 256 caracteres |

Os filtros aplicam-se aos registos devolvidos. Em `/metrics`, o resumo de pedidos
respeita o intervalo, `node` e `workload`; os restantes filtros não filtram esse resumo.

Exemplos:

```http
GET /api/celld/fleets/f123/logs?severity=ERROR&source=otlp&limit=100
GET /api/celld/fleets/f123/logs?node=127.0.0.2&from=1792316400000&to=1792320000000
GET /api/celld/fleets/f123/events?limit=100
GET /api/celld/fleets/f123/traces/0123456789abcdef0123456789abcdef?from=1792316400000&to=1792320000000
```

Trace IDs: 32 caracteres hexadecimais em minúsculas. Usar um intervalo que
inclua o evento selecionado; o endpoint de traces também começa na última hora
se não forem enviados `from`/`to`.

Para continuar: enviar `cursor=next_cursor` com os filtros originais. O cursor
fixa `to`; é possível omitir `to` nas páginas seguintes. Ao mudar filtros ou
intervalo, descartar o cursor. `truncated: true` pode resultar do limite de linhas
ou bytes; seguir `next_cursor` quando existir.

## Métricas

```json
{
  "samples": {
    "records": [{
      "id": "sample-opaco", "node": "127.0.0.2",
      "generation": "0123456789abcdef0123456789abcdef",
      "received_ms": 1792320005000,
      "snapshot": {
        "fleet_id": "f123", "node_id": "127.0.0.2",
        "collected_ms": 1792320000000, "state": "fresh",
        "resources": {"rss_bytes": 52428800, "cpu_percent_x100": 1250}
      }
    }],
    "from": 1792316400000, "to": 1792320000000,
    "truncated": false, "incomplete": true
  },
  "requests": {
    "count": 120, "errors": 3,
    "mean_duration_us": 8000, "max_duration_us": 250000,
    "rate_per_second": 0.03333333333333333,
    "observed": true, "incomplete": true
  }
}
```

Gráficos: usar `samples.records[].snapshot.resources`; o histórico também tem
paginação em `samples.next_cursor`. O resumo conta spans de servidor observados
no intervalo completo, não apenas na página de amostras. Não são totais exatos
nem percentis de toda a fleet.

## Download

`GET /celld/fleets/{id}/logs?download=true&limit=2000&from=...&to=...`
retorna **NDJSON**, uma linha por `StoredObservation`, com limite de 2.000 linhas
e até 1 MiB. Por omissão continua a devolver apenas 200 linhas se `limit` for omitido.

Headers: `X-Observability-Truncated: true` quando limitado; `X-Next-Cursor` quando
há continuação. Em browser com origem diferente, o CORS atual não expõe estes
headers personalizados ao JavaScript: preferir paginação JSON para conhecer a
continuação, ou usar a API pela mesma origem. O ficheiro descarregado é um
export limitado, não a garantia de todo o histórico.

## Exemplo TypeScript

```ts
const base = "/api"; // ou a base já usada pela aplicação
const fleetPath = `/celld/fleets/${encodeURIComponent(fleetId)}`;

async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch(`${base}${path}`, {
    headers: { Authorization: `Bearer ${token}` },
    signal,
    cache: "no-store",
  });
  // A autenticação pode devolver texto simples, não JSON.
  if (response.status === 401) throw new Error("Sessão expirada");
  if (!response.ok) throw new Error(await response.text());
  return response.json(); // Validar o schema no service real da aplicação.
}

const overview = await getJSON<FleetObservability>(`${fleetPath}/observability`);
const query = new URLSearchParams({
  from: String(Date.now() - 60 * 60 * 1000),
  to: String(Date.now()),
  severity: "ERROR",
  limit: "100",
});
const first = await getJSON<ObservationPage>(`${fleetPath}/logs?${query}`);
if (first.next_cursor) {
  query.set("cursor", first.next_cursor);
  const next = await getJSON<ObservationPage>(`${fleetPath}/logs?${query}`);
}
```

Definir `FleetObservability`/`ObservationPage` conforme os envelopes acima.
Campos opcionais de recursos/workloads devem ser `number | undefined`, e os
arrays opcionais normalizados com `?? []`. O backend usa números JSON e nomes
`snake_case`, incluindo inteiros dos protobufs. Escapar parâmetros com
`URLSearchParams`; não construir filtros por concatenação de valores livres.

## Regras essenciais de UI

- Logs/eventos em follow usam SSE autenticado; cancelar ao pausar/fechar o detalhe.
  Recursos continuam recolhidos a cada 5 s; deployment metadata pode demorar 30 s.
  Em agentes atualizados, o caminho de logs é OTLP (flush 250 ms) → tail gRPC →
  histórico central → SSE. O tempo real inclui execução, persistência e rede.
- Deduplicar por `id` e manter o buffer de 2.000 registos. O histórico mantém
  paginação com intervalo fixo; o stream usa receipt timestamps para recuperar
  registos após uma quebra. APIs/clientes antigos usam polling identificado na UI.
- Estado do nó: `pending`, `fresh`, `partial`, `stale`, `offline`, `unsupported`.
  Deployment: `pending`, `fresh`, `stale`, `unavailable`.
  Capacidades: `supported`, `partial`, `unsupported`, `unavailable`; aceitar
  estados novos. Mostrar o `detail` correspondente quando houver limitações.
- Campos opcionais ausentes são desconhecidos/não suportados; não usar `|| 0`.
  Um `0` ou `false` explícito é um valor válido. Os escalares não opcionais de
  record/snapshot também podem ser omitidos quando têm o valor zero por omissão.
- Datas em ms; bytes para memória; `duration_us / 1000` para ms;
  `cpu_percent_x100 / 100` para percentagem. Containers usam `cpu_percent`
  diretamente. Não fazer média das percentagens dos nós.
- Mostrar `collected_ms` e preservar o último valor quando estiver stale.
  Depois de 15 s, amostras antigas são marcadas stale pelo backend.
- `resident_cells` é subconjunto de `owned_cells`. Pools e células internas são
  uma decomposição; não somar linhas de workloads às contagens dos recursos.
  Workflows em espera não implicam falha; facets não contam como células próprias.
- Mostrar `error` de topo, nó, deployment, storage, página e resumo de pedidos.
  Pode haver HTTP 200 com dados parciais/erro. `incomplete: true` é esperado na
  implementação atual, incluindo sem perda conhecida; significa observações não
  garantidas como totais exatos. Não é, por si só, estado de falha.
- Mostrar perdas (`nodes[].dropped`, `storage.dropped`), tentativas recusadas
  (`storage.rejected_batches`) e limpeza (`storage.pruned_segments`). São sinais
  diferentes; uma tentativa recusada pode ter sucesso no retry.
- Histórico: até 7 dias, podendo ser menor por quota/limpeza; `historical: true`
  significa fleet já removida, mas ainda com histórico. Não oferecer ações de
  lifecycle sobre uma fleet histórica. Após expiração, recebe-se 404.
- Limitações: sem backlog de queues, censo global/steps de Workflows ou histórico
  exato de cron. Alguns erros lançados por Workers têm logs com trace ID mas sem
  span correspondente. Mostrar “sem spans disponíveis” em vez de fabricar um trace.

Erros HTTP: **400** query inválida; **401** sessão inválida; **404** fleet/ID
histórico inexistente ou expirado; **503** serviço indisponível. Erros JSON de
rotas têm `{ "error": "..." }`; 401 pode ser texto simples. Um trace sem registos
num intervalo válido devolve uma página vazia, não necessariamente 404.

## Application and celld log views

The Logs view opens with **Application** selected (`source=otlp`). Console output
from application handlers is collected separately from the celld process streams.
The visible source choices are Application, celld, Containers and All logs.
**celld** sends `source=process`, which groups `stdout` and `stderr` in the master
query before pagination and NDJSON export limits. Existing exact source filters
continue to work. Stored record sources remain unchanged.

Each message shows its origin (Application, celld stdout/stderr, or Container),
and expanded details retain the raw source. Source selection survives clear-filter
and time-range actions. Switching origins clears the workload filter because
application console records can lack workload metadata and process logs do not
identify an application. Saved explicit searches are restored unchanged.

The grouped `source=process` filter requires the updated master. Application
selection uses the existing `source=otlp` contract. These are local source changes;
a successful build/test does not establish that a deployed UI/master was updated.

## Authenticated live streams

`GET /celld/fleets/{id}/logs/stream` and `/events/stream` return SSE with the
same bearer-header authentication as the JSON endpoints. The frontend consumes
streaming `fetch`, avoiding bearer tokens in URLs. Existing node, workload,
source (including `process`), severity and literal text filters apply before live
fanout. `from` is the lower event-time boundary; `to` validates the initial
window, while live delivery continues beyond it. `cursor`/`download` are rejected.

Optional `since` is an inclusive central receipt timestamp in milliseconds,
limited to the retained seven days. Subscribe precedes replay so commits during
recovery are also queued. Event types:

- `ready`: subscription opened, `received_ms` identifies its server-time boundary.
- `history`: replayed ObservationPage; merge by opaque ID but do **not** advance
  the reconnect checkpoint until replay has completed.
- `caught_up`: `{received_ms, from, to}`; the complete-replay checkpoint.
- `observations`: newly committed ObservationPage. Advance the checkpoint only
  after accepting the complete frame, using its records' `received_ms` values.
- `heartbeat`: keepalive every 15 seconds.
- `reconnect`: slow reader or temporarily busy history; reconnect with the last
  safe checkpoint. `gap`: replay exceeded 2,000 records / 2 MiB; report it and let
  the operator inspect history instead of silently claiming complete recovery.

Replay and subscription can overlap; deduplicate IDs. Receipt-time recovery
includes delayed records whose event timestamps precede the last displayed log.
Each subscriber holds eight bounded batches. Limits are 128 concurrent streams
and 16 per fleet. Slow subscribers are disconnected without blocking ingestion.
Connections rotate every five minutes to revalidate authentication. Responses
set `X-Accel-Buffering: no`, flush each event and bound individual writes to ten
seconds. The existing HTTP logger unwraps its writer for flushing/deadlines.

The client reconnects with backoff, cancels on pause/filter/token/close changes,
and keeps messages already shown. Unsupported endpoints or clients without a
readable response body use a labelled two-second polling fallback. Native
streaming support depends on the fetch implementation; the web path is verified.

Deploy updated agents, master and frontend for the full low-latency path. The
agent tail never discards unacknowledged records; the master acknowledges only
after successful persistence. Existing unary collection remains for snapshots,
recovery and old agents; their records still stream from the master but retain
the upstream collection delay. Restart fleet processes to apply the new 250ms
OTLP environment setting. These changes do not perform a rollout.
