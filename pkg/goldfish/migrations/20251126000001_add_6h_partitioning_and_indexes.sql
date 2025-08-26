-- +goose Up
-- Add 6-hour partitioning and performance indexes for goldfish query optimization

-- First, add all the missing indexes
CREATE INDEX idx_sampled_queries_user ON sampled_queries(user);

-- Composite index for the JOIN operation with comparison_outcomes
CREATE INDEX idx_sampled_queries_correlation_composite ON sampled_queries(
    correlation_id,
    cell_a_status_code,
    cell_b_status_code,
    cell_a_response_hash,
    cell_b_response_hash
);

-- Composite index for filtering queries with WHERE and HAVING clauses
CREATE INDEX idx_sampled_queries_filter_composite ON sampled_queries(
    tenant_id,
    user,
    sampled_at DESC,
    correlation_id
);

-- Index for new engine filtering
CREATE INDEX idx_sampled_queries_engine_filter ON sampled_queries(
    cell_a_used_new_engine,
    cell_b_used_new_engine,
    sampled_at DESC
);

-- Index for comparison_outcomes join performance
CREATE INDEX idx_comparison_outcomes_correlation_status ON comparison_outcomes(
    correlation_id, 
    comparison_status
);

-- Convert the table to use 6-hour partitioning for optimal performance
-- Using UNIX_TIMESTAMP for more granular partitioning
ALTER TABLE sampled_queries 
PARTITION BY RANGE (UNIX_TIMESTAMP(sampled_at)) (
    -- Create partitions for the last 7 days (28 partitions at 6 hours each)
    PARTITION p_old VALUES LESS THAN (UNIX_TIMESTAMP(DATE_SUB(NOW(), INTERVAL 7 DAY))),
    PARTITION p_2025_11_24_00 VALUES LESS THAN (UNIX_TIMESTAMP('2025-11-24 06:00:00')),
    PARTITION p_2025_11_24_06 VALUES LESS THAN (UNIX_TIMESTAMP('2025-11-24 12:00:00')),
    PARTITION p_2025_11_24_12 VALUES LESS THAN (UNIX_TIMESTAMP('2025-11-24 18:00:00')),
    PARTITION p_2025_11_24_18 VALUES LESS THAN (UNIX_TIMESTAMP('2025-11-25 00:00:00')),
    PARTITION p_2025_11_25_00 VALUES LESS THAN (UNIX_TIMESTAMP('2025-11-25 06:00:00')),
    PARTITION p_2025_11_25_06 VALUES LESS THAN (UNIX_TIMESTAMP('2025-11-25 12:00:00')),
    PARTITION p_2025_11_25_12 VALUES LESS THAN (UNIX_TIMESTAMP('2025-11-25 18:00:00')),
    PARTITION p_2025_11_25_18 VALUES LESS THAN (UNIX_TIMESTAMP('2025-11-26 00:00:00')),
    PARTITION p_2025_11_26_00 VALUES LESS THAN (UNIX_TIMESTAMP('2025-11-26 06:00:00')),
    PARTITION p_2025_11_26_06 VALUES LESS THAN (UNIX_TIMESTAMP('2025-11-26 12:00:00')),
    PARTITION p_2025_11_26_12 VALUES LESS THAN (UNIX_TIMESTAMP('2025-11-26 18:00:00')),
    PARTITION p_2025_11_26_18 VALUES LESS THAN (UNIX_TIMESTAMP('2025-11-27 00:00:00')),
    PARTITION p_current VALUES LESS THAN (UNIX_TIMESTAMP('2025-11-27 06:00:00')),
    PARTITION p_future VALUES LESS THAN MAXVALUE
);

-- Create stored procedure for automatic 6-hour partition management
DELIMITER //

CREATE PROCEDURE manage_sampled_queries_partitions_6h()
BEGIN
    DECLARE next_partition_time DATETIME;
    DECLARE partition_name VARCHAR(64);
    DECLARE old_partition_time DATETIME;
    DECLARE partition_exists INT;
    DECLARE done INT DEFAULT FALSE;
    DECLARE cur_partition_name VARCHAR(64);
    
    -- Calculate the next 6-hour boundary
    SET next_partition_time = DATE_ADD(
        DATE_FORMAT(NOW(), '%Y-%m-%d %H:00:00'),
        INTERVAL (FLOOR(HOUR(NOW()) / 6) + 1) * 6 HOUR
    );
    
    -- Create partition name (e.g., p_2025_11_26_18 for 6PM)
    SET partition_name = CONCAT('p_', 
        DATE_FORMAT(next_partition_time, '%Y_%m_%d_%H'));
    
    -- Check if the partition already exists
    SELECT COUNT(*) INTO partition_exists
    FROM INFORMATION_SCHEMA.PARTITIONS
    WHERE TABLE_NAME = 'sampled_queries'
    AND TABLE_SCHEMA = DATABASE()
    AND PARTITION_NAME = partition_name;
    
    -- Create the next partition if it doesn't exist
    IF partition_exists = 0 THEN
        -- Reorganize p_future to create new partition
        SET @sql = CONCAT('ALTER TABLE sampled_queries ',
                         'REORGANIZE PARTITION p_future INTO (',
                         'PARTITION ', partition_name, ' VALUES LESS THAN (UNIX_TIMESTAMP(\'', 
                         DATE_ADD(next_partition_time, INTERVAL 6 HOUR), '\')), ',
                         'PARTITION p_future VALUES LESS THAN MAXVALUE)');
        PREPARE stmt FROM @sql;
        EXECUTE stmt;
        DEALLOCATE PREPARE stmt;
    END IF;
    
    -- Drop partitions older than 7 days (keeping 28 6-hour partitions)
    SET old_partition_time = DATE_SUB(NOW(), INTERVAL 7 DAY);
    
    -- Use cursor to iterate through old partitions
    BEGIN
        DECLARE partition_cursor CURSOR FOR 
            SELECT PARTITION_NAME 
            FROM INFORMATION_SCHEMA.PARTITIONS
            WHERE TABLE_NAME = 'sampled_queries'
            AND TABLE_SCHEMA = DATABASE()
            AND PARTITION_NAME LIKE 'p_20%'  -- Date-based partitions only
            AND PARTITION_NAME < CONCAT('p_', DATE_FORMAT(old_partition_time, '%Y_%m_%d_%H'))
            AND PARTITION_NAME != 'p_old';  -- Don't drop the old data partition
        
        DECLARE CONTINUE HANDLER FOR NOT FOUND SET done = TRUE;
        
        OPEN partition_cursor;
        
        read_loop: LOOP
            FETCH partition_cursor INTO cur_partition_name;
            IF done THEN
                LEAVE read_loop;
            END IF;
            
            -- Drop the old partition
            SET @sql = CONCAT('ALTER TABLE sampled_queries DROP PARTITION ', cur_partition_name);
            PREPARE stmt FROM @sql;
            EXECUTE stmt;
            DEALLOCATE PREPARE stmt;
        END LOOP;
        
        CLOSE partition_cursor;
    END;
END//

DELIMITER ;

-- Create an event to run partition management every hour
-- This ensures we always have partitions ready before data arrives
CREATE EVENT manage_goldfish_partitions_6h
ON SCHEDULE EVERY 1 HOUR
STARTS DATE_FORMAT(DATE_ADD(NOW(), INTERVAL 1 HOUR), '%Y-%m-%d %H:00:00')
DO CALL manage_sampled_queries_partitions_6h();

-- Index optimized for partition pruning with UNIX_TIMESTAMP
CREATE INDEX idx_sampled_queries_unix_timestamp 
ON sampled_queries(sampled_at, correlation_id);

-- +goose Down
-- Remove partitioning and indexes

-- Drop the event and stored procedure
DROP EVENT manage_goldfish_partitions_6h;
DROP PROCEDURE manage_sampled_queries_partitions_6h;

-- Remove partitioning (converts back to regular table)
ALTER TABLE sampled_queries REMOVE PARTITIONING;

-- Drop all the new indexes
DROP INDEX idx_sampled_queries_unix_timestamp ON sampled_queries;
DROP INDEX idx_comparison_outcomes_correlation_status ON comparison_outcomes;
DROP INDEX idx_sampled_queries_engine_filter ON sampled_queries;
DROP INDEX idx_sampled_queries_filter_composite ON sampled_queries;
DROP INDEX idx_sampled_queries_correlation_composite ON sampled_queries;
DROP INDEX idx_sampled_queries_user ON sampled_queries;
