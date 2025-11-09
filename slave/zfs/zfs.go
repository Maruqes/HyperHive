package zfs
/*
gpt explanation :D

ZFS Overview & Planned Usage
----------------------------
Core Concepts:
  - A **pool** (zpool) is a logical storage group that the OS sees as a single volume.
  - Each pool is built from one or more **vdevs** (virtual devices).
  - Each vdev contains one or more **disks** (physical drives).
  - Data redundancy and layout (mirror, raidz1/2/3, stripe) are defined per vdev.
  - The pool distributes data across all vdevs, handles checksums, compression,
    caching, and ensures end-to-end data integrity.

Important Notes:
  - **VDEVs are immutable:** once created, you cannot add or remove disks from a vdev,
    or change its RAID type (e.g., from raidz1 to raidz2). You can only replace failed
    or smaller drives with new ones of equal or larger size.
  - **Pools are expandable:** you can add new vdevs (e.g., another RAIDZ group) to grow
    total capacity. ZFS will automatically balance new writes across all vdevs.
  - **Disk replacement is online:** if a drive fails (marked FAULTED/DEGRADED),
    insert a new one and run `zpool replace`. ZFS performs a "resilver" —
    rebuilding only the used data blocks, keeping the system online.
  - **Automatic repair:** ZFS uses checksums on all data and metadata. If a block
    is corrupted, it can automatically self-heal using redundant copies.
  - **Expansion:** once all drives in a vdev have been replaced with larger ones,
    the vdev and pool automatically grow in capacity.

What we’ll do:
  1. Create one or more ZFS pools (e.g., 'mcpool') with RAIDZ2 redundancy.
  2. Within each pool, create datasets (e.g., 'mcpool/mundos') for Minecraft worlds
     or other server data, with properties like:
       compression = zstd
       atime = off
       recordsize = 128K
  3. Use Go (via github.com/bicomsystems/go-libzfs or CLI wrappers) to manage:
       - Pool health checks and scrubs
       - Disk replacement automation
       - Dataset creation, snapshots, and replication
  4. Treat the pool as a single logical disk — ZFS handles redundancy,
     caching, block-level verification, and live recovery automatically.

Result:
  A single logical storage system with built-in RAID, integrity verification,
  and self-healing — ideal for production workloads (like Minecraft worlds),
  high availability, and future scalability.
*/
