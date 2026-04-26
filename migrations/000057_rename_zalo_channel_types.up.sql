-- Rename Zalo channel types in channel_instances to align with Zalo's
-- own product taxonomy. Pre-refactor names inverted reality:
--   'zalo_oa'     → static-token Bot variant (actually "zalo_bot")
--   'zalo_oauth'  → phone-tied Official Account via OAuth (the canonical "zalo_oa")
--
-- Three-step swap via zalo_oa_tmp sentinel avoids transient collision even
-- though channel_type has no unique constraint today.
--
-- Idempotency guard: only swap when legacy 'zalo_oauth' rows still exist.
-- golang-migrate's version table prevents normal re-run, but a manual
-- `migrate force <prev> && migrate up` on a post-deploy DB would silently
-- re-flip the new 'zalo_oa' rows back to 'zalo_bot' at step 2. The guard
-- makes the migration a no-op once it has been applied.

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM channel_instances WHERE channel_type = 'zalo_oauth') THEN
    UPDATE channel_instances SET channel_type = 'zalo_oa_tmp' WHERE channel_type = 'zalo_oauth';
    UPDATE channel_instances SET channel_type = 'zalo_bot'    WHERE channel_type = 'zalo_oa';
    UPDATE channel_instances SET channel_type = 'zalo_oa'     WHERE channel_type = 'zalo_oa_tmp';
  END IF;
END $$;
