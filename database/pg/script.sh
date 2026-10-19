CREATE TABLE test_data (
    id BIGSERIAL PRIMARY KEY,
    event_type VARCHAR(50),
    payload TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE OR REPLACE PROCEDURE fill_db_to_capacity()
LANGUAGE plpgsql
AS $$
BEGIN
    LOOP
        INSERT INTO test_data (event_type, payload)
        SELECT 'STRESS_TEST', repeat('A', 1024) 
        FROM generate_series(1, 10000);
        
        COMMIT;
    END LOOP;
END;
$$;

CALL fill_db_to_capacity();