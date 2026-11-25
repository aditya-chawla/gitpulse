-- Revert Milestone 0, step 3: Action queue.

DROP INDEX IF EXISTS action_items_active_idx;
DROP TABLE IF EXISTS action_items;
