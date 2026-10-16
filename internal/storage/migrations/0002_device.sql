-- Migration 0002 : identité de l'appareil, détection de présence et
-- anti-duplication des événements matériels.
--
-- Choix structurants :
--   * `device` est une ligne unique (CHECK id = 1) : l'identité d'un appareil
--     REMPLACE la précédente, elle ne s'accumule pas. Un historique d'identités
--     n'aurait aucun sens pour une supervision mono-onduleur.
--   * les états de détection sont stockés en texte à trois valeurs
--     (unknown/absent/present) et non en booléens : « non déterminé » n'est pas
--     « absent ». Un booléen écraserait cette distinction, et l'interface
--     afficherait « aucun Smart Meter détecté » après un simple 401 — une
--     absence fabriquée, exactement ce que l'invariant du modèle interdit.
--   * chaque état porte sa CAUSE (`*_reason`), écrite en même temps que lui :
--     « non déterminé » sans motif n'est pas exploitable, et reconstituer la
--     cause à la lecture serait l'inventer.
--   * les colonnes texte d'identité ont pour défaut la chaîne vide, qui signifie
--     « non trouvé dans la réponse ». Elle est convertie en null à la frontière
--     de l'API : jamais en 0, jamais en « inconnu ».
--   * `device_events_seen` est la mémoire d'anti-duplication : une clé d'événement
--     déjà vue ne produit pas une seconde ligne dans `events`. L'index sur
--     seen_at_utc permet à la purge de retirer les marques en même temps que les
--     événements qu'elles dédupliquent.

CREATE TABLE device (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  read_at_utc TEXT NOT NULL,
  source TEXT NOT NULL,
  model TEXT NOT NULL DEFAULT '',
  firmware TEXT NOT NULL DEFAULT '',
  serial_number TEXT NOT NULL DEFAULT '',
  identity_state TEXT NOT NULL DEFAULT 'unknown' CHECK (identity_state IN ('unknown', 'absent', 'present')),
  identity_reason TEXT NOT NULL DEFAULT '',
  meter_state TEXT NOT NULL DEFAULT 'unknown' CHECK (meter_state IN ('unknown', 'absent', 'present')),
  meter_source TEXT NOT NULL DEFAULT '',
  meter_reason TEXT NOT NULL DEFAULT '',
  battery_state TEXT NOT NULL DEFAULT 'unknown' CHECK (battery_state IN ('unknown', 'absent', 'present')),
  battery_source TEXT NOT NULL DEFAULT '',
  battery_reason TEXT NOT NULL DEFAULT '',
  device_events_state TEXT NOT NULL DEFAULT 'unknown' CHECK (device_events_state IN ('unknown', 'absent', 'present')),
  device_events_reason TEXT NOT NULL DEFAULT ''
);

CREATE TABLE device_events_seen (
  key TEXT PRIMARY KEY,
  seen_at_utc TEXT NOT NULL
);

CREATE INDEX device_events_seen_ts ON device_events_seen(seen_at_utc);
