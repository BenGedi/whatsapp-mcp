"""Tests for remove_participant() in whatsapp.py — PR #11 fix."""
import sys
import types
import unittest
from unittest.mock import MagicMock, patch


def _load_whatsapp():
    sys.modules.pop("whatsapp", None)
    # Stub heavy imports so whatsapp.py loads without a real DB / requests.
    for mod in ("requests", "sqlite3"):
        if mod not in sys.modules:
            sys.modules[mod] = MagicMock()
    import whatsapp as wa
    return wa


class TestRemoveParticipantStripping(unittest.TestCase):
    """Whitespace is stripped before validation and before the POST."""

    def setUp(self):
        self.wa = _load_whatsapp()

    def _post_call_kwargs(self, group_jid, participant):
        """Return the json= kwarg passed to requests.post, or None if not called."""
        mock_response = MagicMock()
        mock_response.json.return_value = {"success": True, "message": "ok"}
        with patch.object(self.wa.requests, "post", return_value=mock_response) as mock_post:
            self.wa.remove_participant(group_jid, participant)
            if mock_post.called:
                return mock_post.call_args.kwargs.get("json") or mock_post.call_args[1].get("json")
            return None

    def test_strips_group_jid_before_post(self):
        payload = self._post_call_kwargs("  120363000000000001@g.us  ", "972501234567")
        self.assertIsNotNone(payload, "expected a POST to be made")
        self.assertEqual(payload["group_jid"], "120363000000000001@g.us")

    def test_strips_participant_before_post(self):
        payload = self._post_call_kwargs("120363000000000001@g.us", "  972501234567  ")
        self.assertIsNotNone(payload, "expected a POST to be made")
        self.assertEqual(payload["participant"], "972501234567")

    def test_whitespace_only_group_jid_returns_error_not_post(self):
        payload = self._post_call_kwargs("   ", "972501234567")
        self.assertIsNone(payload, "should not POST when group_jid is whitespace-only")

    def test_whitespace_only_participant_returns_error_not_post(self):
        payload = self._post_call_kwargs("120363000000000001@g.us", "   ")
        self.assertIsNone(payload, "should not POST when participant is whitespace-only")

    def test_empty_group_jid_message(self):
        success, msg = self.wa.remove_participant("", "972501234567")
        self.assertFalse(success)
        self.assertIn("group_jid is required", msg)

    def test_empty_participant_message(self):
        success, msg = self.wa.remove_participant("120363000000000001@g.us", "")
        self.assertFalse(success)
        self.assertIn("participant is required", msg)


if __name__ == "__main__":
    unittest.main()
