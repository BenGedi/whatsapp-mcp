"""Tests for environment-variable driven configuration in whatsapp.py."""
import importlib
import os
import sys
import unittest


def _reload_whatsapp():
    """Force a fresh import so module-level constants are re-evaluated."""
    sys.modules.pop("whatsapp", None)
    import whatsapp  # noqa: PLC0415
    return whatsapp


class TestWhatsAppAPIBaseURL(unittest.TestCase):
    def tearDown(self):
        os.environ.pop("WHATSAPP_API_BASE_URL", None)
        sys.modules.pop("whatsapp", None)

    def test_default_url_when_env_unset(self):
        os.environ.pop("WHATSAPP_API_BASE_URL", None)
        mod = _reload_whatsapp()
        self.assertEqual(mod.WHATSAPP_API_BASE_URL, "http://localhost:8080/api")

    def test_custom_url_from_env(self):
        os.environ["WHATSAPP_API_BASE_URL"] = "http://myhost:9090/api"
        mod = _reload_whatsapp()
        self.assertEqual(mod.WHATSAPP_API_BASE_URL, "http://myhost:9090/api")

    def test_env_change_takes_effect_on_reload(self):
        os.environ.pop("WHATSAPP_API_BASE_URL", None)
        mod1 = _reload_whatsapp()
        self.assertEqual(mod1.WHATSAPP_API_BASE_URL, "http://localhost:8080/api")

        os.environ["WHATSAPP_API_BASE_URL"] = "http://other:1234/api"
        mod2 = _reload_whatsapp()
        self.assertEqual(mod2.WHATSAPP_API_BASE_URL, "http://other:1234/api")

    def test_empty_env_falls_back_to_default(self):
        os.environ["WHATSAPP_API_BASE_URL"] = ""
        mod = _reload_whatsapp()
        self.assertEqual(mod.WHATSAPP_API_BASE_URL, "http://localhost:8080/api")

    def test_whitespace_only_env_falls_back_to_default(self):
        os.environ["WHATSAPP_API_BASE_URL"] = "   "
        mod = _reload_whatsapp()
        self.assertEqual(mod.WHATSAPP_API_BASE_URL, "http://localhost:8080/api")


if __name__ == "__main__":
    unittest.main()
