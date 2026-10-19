# Setup

- check if you have 100gb free for this
  -> df -h /

```plpgsql
fallocate -l 100G mysql_limit.img

mkfs.ext4 mysql_limit.img

mkdir -p mysql_data
sudo mount -o loop mysql_limit.img mysql_data

sudo chmod 777 mysql_data
```

## Check

```plpgsql
docker exec -it mysql_limit_test mysql -u root -prootpass testdb

SELECT COUNT(*) FROM test_data;

SELECT
    table_rows AS estimated_row_count,
    CONCAT(ROUND(data_length / (1024 * 1024 * 1024), 2), ' GB') AS data_size,
    CONCAT(ROUND(index_length / (1024 * 1024 * 1024), 2), ' GB') AS index_size,
    CONCAT(ROUND((data_length + index_length) / (1024 * 1024 * 1024), 2), ' GB') AS total_size
FROM information_schema.tables
WHERE table_schema = 'testdb'
  AND table_name = 'test_data';
```

## Number of rows for 100gb

```bash

MYsql
+----------+
| COUNT(*) |
+----------+
| 42400000 |
+----------+
1 row in set (46.92 sec)

+---------------------+-----------+------------+------------+
| estimated_row_count | data_size | index_size | total_size |
+---------------------+-----------+------------+------------+
|            39462283 | 45.31 GB  | 0.00 GB    | 45.31 GB   |
+---------------------+-----------+------------+------------+
1 row in set (0.03 sec)

take like 70min to fullfil
```

## Cleanup

```bash
sudo umount mysql_data
rm mysql_limit.img
rm -rf mysql_data
```
