"""Tests for the PR #10 fix: ORDER BY injection via sort_by in list_chats()."""
import os
import sqlite3
import sys
import tempfile
import unittest


def _make_db(path: str) -> None:
    """Create a minimal messages.db with two chats for ordering tests."""
    conn = sqlite3.connect(path)
    conn.executescript("""
        CREATE TABLE chats (
            jid TEXT PRIMARY KEY,
            name TEXT,
            last_message_time TEXT,
            watched BOOLEAN DEFAULT 0
        );
        CREATE TABLE messages (
            id TEXT PRIMARY KEY,
            chat_jid TEXT,
            timestamp TEXT,
            content TEXT,
            sender TEXT,
            is_from_me INTEGER
        );
        INSERT INTO chats VALUES ('alpha@s.whatsapp.net', 'Alpha', '2026-05-01T10:00:00', 0);
        INSERT INTO chats VALUES ('beta@s.whatsapp.net',  'Beta',  '2026-05-02T10:00:00', 0);
    """)
    conn.commit()
    conn.close()


def _load_whatsapp(db_path: str):
    sys.modules.pop("whatsapp", None)
    import whatsapp as wa
    wa.MESSAGES_DB_PATH = db_path
    return wa


class TestListChatsSortBy(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.NamedTemporaryFile(suffix=".db", delete=False)
        self._tmp.close()
        _make_db(self._tmp.name)
        self.wa = _load_whatsapp(self._tmp.name)

    def tearDown(self):
        sys.modules.pop("whatsapp", None)
        os.unlink(self._tmp.name)

    # Test plan item 1: sort_by="last_active" → ordered by last_message_time DESC
    def test_sort_last_active_orders_by_time_desc(self):
        chats = self.wa.list_chats(sort_by="last_active")
        self.assertEqual(len(chats), 2)
        # beta has the later timestamp so it must come first
        self.assertEqual(chats[0].jid, "beta@s.whatsapp.net")
        self.assertEqual(chats[1].jid, "alpha@s.whatsapp.net")

    # Test plan item 2: sort_by="name" → ordered alphabetically
    def test_sort_name_orders_alphabetically(self):
        chats = self.wa.list_chats(sort_by="name")
        self.assertEqual(len(chats), 2)
        self.assertEqual(chats[0].jid, "alpha@s.whatsapp.net")
        self.assertEqual(chats[1].jid, "beta@s.whatsapp.net")

    # Test plan item 3: injected SQL expression → falls back safely, no error
    def test_injection_payload_falls_back_to_last_active(self):
        payload = "(SELECT name FROM messages LIMIT 1)"
        # Must not raise, must return results, must fall back to last_active order
        chats = self.wa.list_chats(sort_by=payload)
        self.assertEqual(len(chats), 2)
        # Fallback is last_active (DESC by time): beta first
        self.assertEqual(chats[0].jid, "beta@s.whatsapp.net")


if __name__ == "__main__":
    unittest.main(verbosity=2)
