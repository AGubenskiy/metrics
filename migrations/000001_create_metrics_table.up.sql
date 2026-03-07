CREATE TABLE IF NOT EXISTS metrics (
    metric_id TEXT NOT NULL,
    metric_type TEXT NOT NULL CHECK (metric_type IN ('gauge', 'counter')),
    gauge_value DOUBLE PRECISION,
    counter_value BIGINT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (metric_id, metric_type),
    CHECK (
        (metric_type = 'gauge' AND gauge_value IS NOT NULL AND counter_value IS NULL) OR
        (metric_type = 'counter' AND counter_value IS NOT NULL AND gauge_value IS NULL)
    )
);
