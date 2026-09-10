-- v11 (compatible with v3+): Add table mapping WhatsApp usernames to LID/PN
CREATE TABLE whatsapp_username_map (
    lid      TEXT PRIMARY KEY,
    pn       TEXT,
    username TEXT UNIQUE
);
