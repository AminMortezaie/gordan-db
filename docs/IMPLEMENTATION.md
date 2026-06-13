# gordan-db — Implementation Guide

This document is the hands-on build plan. For motivation, architecture overview, and contribution targets, see [readme.md](../readme.md).

---

## Build Order

Phase 1 is ordered for learning and to avoid redesigning flush/recovery semantics later:

```
1. Page Manager        — fixed 4KB pages, pread/pwrite
2. Slotted Page Layout — records + slot directory inside a page
3. Minimal WAL         — append + fsync before page write; crash replay on open
4. Buffer Pool         — cache, dirty tracking, LRU, flush through WAL
5. B+ Tree             — search, insert, splits; delete later
6. Public KV API       — Get / Put / Delete / Iterator
```

**Deferred until core path works:** mmap (read optimization), page freelist/reuse, delete + merge, distributed layer.

**Rule:** Do not start the next layer until the current one has tests and you can explain its invariants without looking at notes.

---

## Phase 1 — Page Manager

### Goals

- Open or create a `.gdb` file
- Read and write pages by ID (fixed **4096 bytes**)
- Use explicit `pread` / `pwrite` everywhere (no mmap until correctness is proven)

### Page ID → file offset

```
offset = pageID × PageSize    // PageSize = 4096
```

Page 0 is valid. File grows on first write to a new page ID (extend file with zero-filled pages if needed).

### Suggested API

```go
type Manager struct { /* file handle, path, mutex */ }

func Open(path string) (*Manager, error)
func (m *Manager) ReadPage(pageID uint64) ([]byte, error)   // returns 4096 bytes
func (m *Manager) WritePage(pageID uint64, data []byte) error // len(data) must be 4096
func (m *Manager) Close() error
```

### Tests (required before moving on)

- [ ] Create file, write page N, close, reopen, read page N — bytes match
- [ ] Write multiple non-contiguous page IDs; file size is correct
- [ ] `WritePage` rejects data that is not exactly 4096 bytes
- [ ] Read from unwritten page returns zeros or explicit error (pick one, document it)

### Done when

You can `hexdump -C file.gdb` after writes and see predictable 4KB blocks at expected offsets.

---

## Slotted Page Layout

Define what lives *inside* a page before building the B+ tree. This matches the model used by Bolt/bbolt.

### Memory layout

```
┌──────────────────────────────────────────────┐
│ Header (fixed size, e.g. 16 bytes)           │
│   - page type (meta / leaf / internal)       │
│   - flags                                      │
│   - item count (uint16)                        │
│   - free space pointer (uint16)                │
├──────────────────────────────────────────────┤
│ Slot directory (grows down from top)           │
│   uint16 offsets to records, one per item      │
├──────────────────────────────────────────────┤
│                    free space                  │
├──────────────────────────────────────────────┤
│ Record area (grows up from bottom)           │
│   [recordLen: uint16][payload...]            │
└──────────────────────────────────────────────┘
```

Slots point to the start of each record in the record area. Inserts allocate from free space between the slot directory and the record area.

### Operations

- `Insert(record []byte) (slotIndex int, err error)` — fail with `ErrPageFull` when no room
- `Get(slotIndex int) ([]byte, error)`
- `Delete(slotIndex int) error` — optional in v1; defer until B+ tree delete

### Tests

- [ ] Insert one record, read back — payload matches
- [ ] Insert until page full → error
- [ ] Item count and free pointer update correctly after each insert
- [ ] Binary layout is stable (golden test or hex dump snapshot)

### Done when

You can pack variable-length byte slices into a single 4KB page and read them back by slot index.

---

## Minimal WAL

### Invariant

**No page is written to the `.gdb` file until its WAL record is fsync'd.**

This is the durability guarantee. The buffer pool and B+ tree must never call `WritePage` on the data file without going through WAL first.

### Record format (v0 — keep simple)

Log full page images for clarity; optimize later.

```
[type:1 byte][pageID:8 bytes LE][pageData:4096 bytes]
```

Optional: add CRC32 at end of record for corruption detection.

### Lifecycle

1. **Append** — write record to WAL file
2. **Fsync** — `Sync()` WAL before any data-file write
3. **Apply** — `PageManager.WritePage(pageID, pageData)`
4. **Checkpoint** — after successful flush, truncate or rotate WAL (can defer rotation until buffer pool exists)

### Recovery on `Open()`

1. Open data file + WAL
2. Scan WAL sequentially; for each complete record, apply to page manager (or in-memory page cache)
3. If WAL ends mid-record, stop at last complete record (or fail safe — document behavior)
4. Database is consistent up to last synced WAL entry

### Tests

- [ ] Write page via WAL path, reopen — data present
- [ ] WAL fsync ordering: mock or trace that `Sync()` happens before data-file `pwrite`
- [ ] Truncate WAL mid-record, reopen — no panic; last complete state recovered
- [ ] Empty WAL on fresh DB — no-op recovery

### Done when

You can kill the process after WAL append + fsync but before data write, reopen, and still read the page.

---

## Buffer Pool

Build **after** WAL so the flush path is fixed from the start:

```
mark dirty → WAL append + fsync → PageManager.WritePage → unmark dirty
```

### Responsibilities

- `Fetch(pageID)` — return cached page or load from disk via Page Manager
- `NewPage(pageID)` — allocate zeroed page in cache
- Track dirty pages
- LRU eviction for **clean** pages only
- Never evict a dirty page without flushing through WAL first
- `Flush(pageID)` / `FlushAll()`

### Suggested structure

```go
type Pool struct {
    pages    map[uint64]*frame   // pageID → cached frame
    lru      *list.List          // clean pages, MRU at front
    dirty    map[uint64]struct{}
    capacity int                 // max frames in memory
}
```

### Tests

- [ ] Cache hit: second `Fetch` does not read disk (use counter or mock)
- [ ] Cache miss loads from disk
- [ ] Eviction removes least recently used **clean** page
- [ ] Dirty page flushed before eviction; after flush, page is clean
- [ ] `FlushAll` persists all dirty pages and clears dirty set

### Done when

Repeated reads hit cache; writes go WAL → disk; eviction does not lose data.

---

## B+ Tree

### v1 scope

- Search by key
- Insert (leaf split, internal split / root split)
- **Single writer** — one goroutine owns mutations, or global write lock

### Defer

- Delete and merge/rebalance until insert + search are stable across reopen
- Iterator until leaf sibling links (`nextPageID` on leaves) work reliably

### Page types (use slotted layout)

| Type     | Contents                                      |
|----------|-----------------------------------------------|
| Meta     | Root page ID, page size, version              |
| Internal | Separator keys + child page IDs               |
| Leaf     | Keys + values (+ optional next leaf page ID)  |

### Insert outline

1. Search from root to leaf
2. Insert into leaf if space available
3. If leaf full → split leaf, insert separator into parent (recursive)
4. If root splits → new root with two children, increment tree height

### Tests

- [ ] Empty tree → insert → get returns value
- [ ] Multiple keys, search hits and misses
- [ ] Insert enough keys to force leaf split
- [ ] Insert enough to force internal split and root split
- [ ] Reopen after inserts — all keys still found (integration with WAL + buffer + pages)

### Concurrency

- **Single writer** from day one (matches bbolt)
- Readers: `RWMutex` at tree level initially; refine only if needed

### Done when

Arbitrary insert order works, tree height grows correctly, and data survives reopen.

---

## Public KV API

Wrap the B+ tree in a small `DB` type:

```go
type DB struct { /* page manager, wal, pool, tree */ }

func Open(path string) (*DB, error)
func (db *DB) Close() error

func (db *DB) Put(key, value []byte) error
func (db *DB) Get(key []byte) ([]byte, error)
func (db *DB) Delete(key []byte) error   // after tree delete

type Iterator interface {
    Next() bool
    Key() []byte
    Value() []byte
    Error() error
}
func (db *DB) NewIterator() Iterator     // after leaf chain exists
```

### Tests

- [ ] Put/Get round-trip
- [ ] Get missing key → clear error (e.g. `ErrNotFound`)
- [ ] Close and reopen — all keys still present
- [ ] Concurrent reads while single writer (if exposed)

---

## Testing Strategy

| Layer        | Focus                                           |
|--------------|-------------------------------------------------|
| Page Manager | Byte-exact I/O, reopen persistence              |
| Slotted page | Layout, page full, insert/get                   |
| WAL          | Crash recovery, fsync-before-write ordering     |
| Buffer Pool  | Hit/miss, LRU, dirty flush via WAL              |
| B+ Tree      | Splits, height, search correctness              |
| Integration  | Put/Get, reopen after simulated crash           |

### Crash tests

Add as soon as WAL + page writes exist:

- Write → sync WAL → kill before data write → reopen → verify
- Write → no sync → reopen → document expected loss (only synced state durable)

Use temp dirs (`t.TempDir()`) and subprocess tests if you need real process exit simulation.

---

## Optimizations (later)

| Optimization        | When                                      |
|---------------------|-------------------------------------------|
| mmap for reads      | After pread/pwrite path is correct        |
| WAL record compression / delta | After full-page WAL works        |
| Freelist / page reuse | Study bbolt `freelist.go` first         |
| Checksums on pages  | Before trusting production-like workloads |
| Benchmarks vs bbolt | Phase 3                                   |

---

## Contribution Ladder (bbolt)

1. Read bbolt while building: `page.go`, `node.go`, `freelist.go`, `db.go`
2. Run bbolt benchmarks locally; note where time and allocations go
3. First PR: failing test, doc fix, or small perf fix — not a freelist rewrite
4. Later: freelist / space amplification proposals backed by experiments from gordan-db

---

## Current Status

- [ ] Page Manager
- [ ] Slotted page layout
- [ ] Minimal WAL
- [ ] Buffer Pool
- [ ] B+ Tree (insert + search)
- [ ] KV API (Put / Get)
- [ ] B+ Tree delete
- [ ] Iterator
- [ ] Crash test suite
- [ ] mmap read path
- [ ] Benchmarks vs bbolt
