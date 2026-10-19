# Notes

Reference numbers from the original fill-the-disk Docker tests (removed; see
git history, initial commit). They used a 100 GB loopback disk, 1 KB payloads,
and different insert strategies per DB (PG: 10k rows/statement, MySQL: 1 row/statement),
so they are NOT a fair speed comparison.

| DB | Rows at ~full | Footprint |
|---|---|---|
| Postgres 16 | ~82.5M | ~92 GB |
| MySQL 8.0 | ~42.4M | ~45 GB (data) |
