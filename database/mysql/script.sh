CREATE TABLE test_data (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    event_type VARCHAR(50),
    payload TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

DELIMITER //

CREATE PROCEDURE fill_db_to_capacity()
BEGIN
    DECLARE v_max INT DEFAULT 10000;
    DECLARE v_counter INT DEFAULT 0;
    
    WHILE 1=1 DO
        START TRANSACTION;
        SET v_counter = 0;
        
        WHILE v_counter < v_max DO
            INSERT INTO test_data (event_type, payload) VALUES ('STRESS_TEST', REPEAT('A', 1024));
            SET v_counter = v_counter + 1;
        END WHILE;
        
        COMMIT;
    END WHILE;
END //

DELIMITER ;

CALL fill_db_to_capacity();