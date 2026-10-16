-- 0005_device_name.sql
--
-- Two identity fields that the capture of 2026-10-16 found in /status/version
-- beside the serial number: the device name ("Primo GEN24 3.6 Plus") and the
-- article number. They are stored like model and firmware — an empty string
-- when the device does not report them — and the API translates the empty
-- string into JSON null, so an absent value stays absent instead of becoming an
-- invented name.
--
-- device_name is the name of the unit, not its model: no model key exists in
-- that payload, and the two must not be confused (docs/UNKNOWNS.md #3).
ALTER TABLE device ADD COLUMN device_name TEXT NOT NULL DEFAULT '';
ALTER TABLE device ADD COLUMN article_number TEXT NOT NULL DEFAULT '';
