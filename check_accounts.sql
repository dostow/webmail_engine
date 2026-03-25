-- Check accounts and their sync settings
SELECT 
    id, 
    email, 
    status, 
    sync_settings->>'auto_sync' as auto_sync, 
    sync_settings->>'sync_interval' as sync_interval,
    sync_settings
FROM accounts;
