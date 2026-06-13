# gordan-db

A key-value storage engine built from scratch in Go — for learning, for depth, and eventually for contributing to real databases like [bbolt](https://github.com/etcd-io/bbolt) and [BadgerDB](https://github.com/dgraph-io/badger).

> **gordan** (گردان) — Persian for *warrior unit*. A small, disciplined force.

---

## Why

Most engineers use databases. Few understand what happens below the query.

This project is a deliberate attempt to close that gap — by building a real storage engine from scratch: page layout, buffer pool, write-ahead log, B+ Tree, and eventually a distributed layer. No shortcuts, no AI-generated code. Every line is written by hand to force genuine understanding.

The secondary goal: use this foundation to make meaningful contributions to production KV stores, specifically targeting known open problems in bbolt (freelist performance, space amplification) and BadgerDB (memory overhead).

---

## Architecture

```
┌─────────────────────────────────┐
│           Public API            │  Get / Put / Delete
├─────────────────────────────────┤
│           B+ Tree               │  Index + key routing
├─────────────────────────────────┤
│          Buffer Pool            │  Page cache + dirty tracking + LRU eviction
├──────────────────┬──────────────┤
│    Page Manager  │     WAL      │  Disk I/O       Write-ahead log
├──────────────────┴──────────────┤
│           Disk (.gdb file)      │
└─────────────────────────────────┘
```

---

## Roadmap

### Phase 1 — Storage Engine
- [ ] **Page Manager** — open/create file, 4KB pages, pread/pwrite
- [ ] **Slotted pages** — record + slot layout inside a page
- [ ] **WAL** — append + fsync before page write, crash recovery
- [ ] **Buffer Pool** — cache, dirty tracking, LRU, flush via WAL
- [ ] **B+ Tree** — insert, search, leaf/internal splits (delete later)

### Phase 2 — Public KV API
- [ ] `Put(key, value []byte) error`
- [ ] `Get(key []byte) ([]byte, error)`
- [ ] `Delete(key []byte) error`
- [ ] Iterator for range scans

### Phase 3 — Benchmarks & Contribution
- [ ] Benchmark suite vs bbolt
- [ ] Identify and contribute to bbolt freelist issue
- [ ] Identify and contribute to BadgerDB memory issue

---

## Design Decisions

### Page Size: 4KB
Matches the OS virtual memory page size. Keeps TLB-friendly access patterns — especially important for B+ Tree leaf scans which benefit from sequential memory layout.

### B+ Tree over LSM
LSM (used by BadgerDB, RocksDB) wins on write throughput. B+ Tree (used by BoltDB) wins on read latency. gordan-db chooses B+ Tree to deeply understand the structure that bbolt is built on — and because the read path is where B+ Trees have a real advantage.

### WAL before flush
Pages are never written to disk before the WAL entry is synced. This is the core durability guarantee: if the process crashes mid-write, the WAL is the recovery paper trail.

### I/O: pread/pwrite first, mmap later
Phase 1 uses explicit `pread`/`pwrite` for a simple, testable I/O path. `mmap` for reads is a later optimization (bbolt's model), added once correctness is proven.

---

## Known Weaknesses in Existing Go KV Stores (Motivation)

| Problem | Where | Status |
|---|---|---|
| Write perf degrades over time with large datasets | bbolt | Open |
| Space amplification 3.5x vs Pebble/LevelDB | bbolt | Open (2024) |
| Freelist copy on every write at large sizes | bbolt | Open |
| High memory usage — OOM on 8GB VM | BadgerDB | Known |
| Read amplification under random workloads | BadgerDB (LSM) | By design |

gordan-db is being built with these in mind. The freelist and space amplification problems in bbolt are the primary contribution targets after Phase 1 is complete.

---

## Project Rules

1. **No AI-generated code.** Claude is used in ASK mode only — design questions, concept clarification, debugging direction. Every line of implementation is written by hand.
2. **Tests alongside implementation.** No "I'll test it later."
3. **Understand before moving.** Don't proceed to buffer pool until page manager is fully understood.
4. **One component at a time.** No parallel work on multiple layers.
5. **AI: guidance only.** Use AI for design questions, concept clarification, debugging direction, and progress review — not for generated implementation code. All implementation is written by hand in the editor.

---

## Getting Started

```bash
git clone https://github.com/AminMortezaie/gordan-db
cd gordan-db
go test ./...
```

Requires Go 1.21+.

For step-by-step build order, page layout, WAL invariants, and tests, see [docs/IMPLEMENTATION.md](docs/IMPLEMENTATION.md).

---

## Structure

```
gordan-db/
├── docs/          # Implementation guide
├── page/          # Page Manager — disk I/O layer
├── buffer/        # Buffer Pool — cache + eviction
├── wal/           # Write-Ahead Log
├── tree/          # B+ Tree index
├── db.go          # Public API (Get/Put/Delete)
└── bench/         # Benchmarks vs bbolt
```

---

## References

- [BoltDB source](https://github.com/boltdb/bolt) — the original, now read-only
- [bbolt](https://github.com/etcd-io/bbolt) — the actively maintained fork by etcd team
- [BadgerDB](https://github.com/dgraph-io/badger) — LSM-based, Dgraph's engine
- [Badger vs BoltDB benchmarks](https://hypermode.com/blog/badger-lmdb-boltdb/)
- [bbolt space amplification issue #863](https://github.com/etcd-io/bbolt/issues/863)
- [bbolt freelist perf issue #640](https://github.com/boltdb/bolt/issues/640)
- [The Design and Implementation of Modern Column-Oriented Database Systems](https://www.cs.umd.edu/~abadi/papers/abadi-column-stores.pdf)

---

## Author

Amin Mortezaie — [github.com/AminMortezaie](https://github.com/AminMortezaie) · [medium.com/@a.mortezaie](https://medium.com/@a.mortezaie)