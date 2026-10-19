# Setup

- check if you have 100gb free for this
  -> df -h /

```plpgsql
fallocate -l 100G db_limit.img

mkfs.ext4 db_limit.img

mkdir -p pg_data
sudo mount -o loop db_limit.img pg_data

sudo chmod 777 pg_data
```

## Check

```plpgsql
docker exec -it pg_limit_test psql -U testuser -d testdb

SELECT COUNT(*) FROM test_data;

SELECT pg_size_pretty(pg_relation_size('test_data')) AS table_data_size,
       pg_size_pretty(pg_indexes_size('test_data')) AS index_size,
       pg_size_pretty(pg_total_relation_size('test_data')) AS total_footprint,
       reltuples::bigint AS estimated_row_count
FROM pg_class WHERE relname = 'test_data';
```

## Number of rows for 100gb

```plpgsql

Postgres
  count
----------
 82570000
(1 row)

 table_data_size | index_size | total_footprint | estimated_row_count
-----------------+------------+-----------------+---------------------
 90 GB           | 1769 MB    | 92 GB           |                  -1
(1 row)

take like 8min to fullfil
```

## Cleanup

```bash
sudo umount pg_data
rm db_limit.img
rm -rf pg_data
```
