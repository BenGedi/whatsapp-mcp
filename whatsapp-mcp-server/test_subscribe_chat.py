"""Tests for subscribe_chat / unsubscribe_chat / resolve_chat_jid in whatsapp.py."""
import sys
import types
import unittest
from unittest.mock import MagicMock, patch


def _load_whatsapp():
    sys.modules.pop("whatsapp", None)
    for mod in ("requests", "audio"):
        if mod not in sys.modules:
            sys.modules[mod] = MagicMock()
    import whatsapp as wa
    return wa


class TestResolveChatJID(unittest.TestCase):
    def setUp(self):
        self.wa = _load_whatsapp()

    def test_jid_passthrough(self):
        jid, err = self.wa.resolve_chat_jid("120363000000000001@g.us")
        self.assertEqual(err, "")
        self.assertEqual(jid, "120363000000000001@g.us")

    def test_bare_phone_number(self):
        jid, err = self.wa.resolve_chat_jid("972501234567")
        self.assertEqual(err, "")
        self.assertEqual(jid, "972501234567@s.whatsapp.net")

    def test_phone_number_with_plus(self):
        jid, err = self.wa.resolve_chat_jid("+972501234567")
        self.assertEqual(err, "")
        self.assertEqual(jid, "972501234567@s.whatsapp.net")

    def test_empty_input_returns_error(self):
        jid, err = self.wa.resolve_chat_jid("   ")
        self.assertIsNone(jid)
        self.assertIn("required", err)

    def test_name_single_match(self):
        mock_conn = MagicMock()
        mock_cursor = MagicMock()
        mock_cursor.fetchall.return_value = [("120363000000000001@g.us", "Work Team")]
        mock_conn.cursor.return_value = mock_cursor
        with patch("whatsapp.sqlite3") as mock_sqlite:
            mock_sqlite.connect.return_value = mock_conn
            mock_sqlite.Error = Exception
            jid, err = self.wa.resolve_chat_jid("Work Team")
        self.assertEqual(err, "")
        self.assertEqual(jid, "120363000000000001@g.us")

    def test_name_no_match_returns_error(self):
        mock_conn = MagicMock()
        mock_cursor = MagicMock()
        mock_cursor.fetchall.return_value = []
        mock_conn.cursor.return_value = mock_cursor
        with patch("whatsapp.sqlite3") as mock_sqlite:
            mock_sqlite.connect.return_value = mock_conn
            mock_sqlite.Error = Exception
            jid, err = self.wa.resolve_chat_jid("Unknown Group")
        self.assertIsNone(jid)
        self.assertIn("No chat found", err)

    def test_name_multiple_matches_returns_error(self):
        mock_conn = MagicMock()
        mock_cursor = MagicMock()
        mock_cursor.fetchall.return_value = [
            ("111@g.us", "Work Team A"),
            ("222@g.us", "Work Team B"),
        ]
        mock_conn.cursor.return_value = mock_cursor
        with patch("whatsapp.sqlite3") as mock_sqlite:
            mock_sqlite.connect.return_value = mock_conn
            mock_sqlite.Error = Exception
            jid, err = self.wa.resolve_chat_jid("Work Team")
        self.assertIsNone(jid)
        self.assertIn("Multiple chats", err)

    def test_hebrew_name_passthrough(self):
        # Hebrew names have no case, just ensure they reach the DB query unchanged.
        mock_conn = MagicMock()
        mock_cursor = MagicMock()
        mock_cursor.fetchall.return_value = [("972501111111@s.whatsapp.net", "עבודה")]
        mock_conn.cursor.return_value = mock_cursor
        with patch("whatsapp.sqlite3") as mock_sqlite:
            mock_sqlite.connect.return_value = mock_conn
            mock_sqlite.Error = Exception
            jid, err = self.wa.resolve_chat_jid("עבודה")
        self.assertEqual(err, "")
        self.assertEqual(jid, "972501111111@s.whatsapp.net")


class TestSubscribeChat(unittest.TestCase):
    def setUp(self):
        self.wa = _load_whatsapp()

    def _mock_post(self, success: bool, message: str):
        mock_response = MagicMock()
        mock_response.json.return_value = {"success": success, "message": message}
        return mock_response

    def test_subscribe_by_jid(self):
        with patch.object(self.wa.requests, "post", return_value=self._mock_post(True, "Subscribed")) as mp:
            ok, msg = self.wa.subscribe_chat("120363000000000001@g.us")
        self.assertTrue(ok)
        payload = mp.call_args[1].get("json") or mp.call_args.kwargs.get("json")
        self.assertEqual(payload["jid"], "120363000000000001@g.us")

    def test_subscribe_by_phone_number(self):
        with patch.object(self.wa.requests, "post", return_value=self._mock_post(True, "Subscribed")) as mp:
            ok, _ = self.wa.subscribe_chat("972501234567")
        self.assertTrue(ok)
        payload = mp.call_args[1].get("json") or mp.call_args.kwargs.get("json")
        self.assertEqual(payload["jid"], "972501234567@s.whatsapp.net")

    def test_subscribe_empty_input_no_post(self):
        with patch.object(self.wa.requests, "post") as mp:
            ok, msg = self.wa.subscribe_chat("  ")
        self.assertFalse(ok)
        mp.assert_not_called()

    def test_subscribe_unknown_name_no_post(self):
        mock_conn = MagicMock()
        mock_cursor = MagicMock()
        mock_cursor.fetchall.return_value = []
        mock_conn.cursor.return_value = mock_cursor
        with patch("whatsapp.sqlite3") as mock_sqlite, patch.object(self.wa.requests, "post") as mp:
            mock_sqlite.connect.return_value = mock_conn
            mock_sqlite.Error = Exception
            ok, msg = self.wa.subscribe_chat("Nonexistent Group")
        self.assertFalse(ok)
        mp.assert_not_called()
        self.assertIn("No chat found", msg)


class TestUnsubscribeChat(unittest.TestCase):
    def setUp(self):
        self.wa = _load_whatsapp()

    def test_unsubscribe_by_jid(self):
        mock_response = MagicMock()
        mock_response.json.return_value = {"success": True, "message": "Unsubscribed"}
        with patch.object(self.wa.requests, "post", return_value=mock_response) as mp:
            ok, msg = self.wa.unsubscribe_chat("120363000000000001@g.us")
        self.assertTrue(ok)
        payload = mp.call_args[1].get("json") or mp.call_args.kwargs.get("json")
        self.assertEqual(payload["jid"], "120363000000000001@g.us")

    def test_unsubscribe_empty_input_no_post(self):
        with patch.object(self.wa.requests, "post") as mp:
            ok, msg = self.wa.unsubscribe_chat("")
        self.assertFalse(ok)
        mp.assert_not_called()


if __name__ == "__main__":
    unittest.main()
