# DNS aliases

`POST /dnsmasq/alias/add` creates or replaces an alias. The two optional fields are independent:

```json
{
  "alias": "app.home",
  "ip": "192.0.2.10",
  "ssh_username": "ubuntu",
  "related_vm": "app-vm"
}
```

- `ssh_username`: manually entered text. It is stored as metadata and does not execute SSH commands.
- `related_vm`: the exact VM `name` from the existing `getAllVms` response. A selection control can use that existing endpoint; an unknown name returns HTTP 400. Inventory lookup failures return HTTP 500 without saving the DNS entry.

Both fields may be omitted or set to `""`. Omitting them on an upsert clears their previous values. `GET /dnsmasq/alias/all` returns both fields on every alias, including empty strings for pre-existing records. The existing alias/IP inputs and the existence-check and removal endpoints retain their contracts.

Metadata is persisted as escaped JSON comments alongside the address directives in `/etc/hyperhive/dnsmasq-aliases.conf`. Existing files need no migration. DNS writes are serialized within the master process.

After a VM is successfully removed through the shared `VirshService.DeleteVM` path, all aliases whose `related_vm` matches that VM name are deleted, followed by one DNS reload. Other aliases, including aliases sharing the same IP, are retained. Failed VM removal leaves DNS records intact. Direct hypervisor deletions outside HyperHive do not invoke this cleanup.

As with existing DNS mutations, configuration is written before services reload. Reload failures do not roll back the file. If DNS cleanup fails after VM removal, the API reports that the VM was deleted and DNS cleanup failed; this is a partial failure, not a failed VM deletion.

This is a backend change. No frontend controls or server deployment are included.
