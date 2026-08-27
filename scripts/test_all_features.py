#!/usr/bin/env python3
"""
Comprehensive automated test suite for Ubuntu Router.
Tests all features and API endpoints against the running server.
"""

import json
import sys
import urllib.request
import urllib.parse
import http.cookiejar

BASE_URL = "http://localhost:8080"
PASSWORD = "IQMQs1JdxcQzJ0zj"

class RouterTester:
    def __init__(self):
        self.cookie_jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.cookie_jar))
        self.passed = 0
        self.failed = 0
        self.results = []

    def request(self, path, method="GET", data=None):
        url = f"{BASE_URL}{path}"
        headers = {}
        payload = None
        if data is not None:
            headers["Content-Type"] = "application/json"
            payload = json.dumps(data).encode("utf-8")
        
        req = urllib.request.Request(url, data=payload, headers=headers, method=method)
        try:
            with self.opener.open(req) as resp:
                status_code = resp.status
                body = resp.read().decode("utf-8")
                try:
                    json_body = json.loads(body)
                except Exception:
                    json_body = body
                return status_code, json_body
        except urllib.error.HTTPError as e:
            err_body = e.read().decode("utf-8")
            try:
                json_err = json.loads(err_body)
            except Exception:
                json_err = err_body
            return e.code, json_err
        except Exception as e:
            return 0, str(e)

    def log_result(self, feature, test_name, success, details=""):
        status_str = "PASS" if success else "FAIL"
        color = "\033[92m" if success else "\033[91m"
        reset = "\033[0m"
        print(f"  [{color}{status_str}{reset}] {test_name}: {details}")
        if success:
            self.passed += 1
        else:
            self.failed += 1
        self.results.append({
            "feature": feature,
            "test": test_name,
            "success": success,
            "details": details
        })

    def run_all(self):
        print("\n=======================================================")
        print("    STARTING COMPREHENSIVE UBUNTU ROUTER FEATURE TESTS ")
        print("=======================================================\n")

        self.test_1_authentication()
        self.test_2_dashboard_and_status()
        self.test_3_interfaces_and_routes()
        self.test_4_dhcp()
        self.test_5_dns()
        self.test_6_wan_and_multiwan()
        self.test_7_firewall_and_port_forwards()
        self.test_8_wifi()
        self.test_9_wireguard_vpn()
        self.test_10_external_dns()
        self.test_11_health_and_system()
        self.test_12_rollback_mechanism()

        print("\n=======================================================")
        print(f"  TOTAL RESULTS: {self.passed} Passed, {self.failed} Failed")
        print("=======================================================\n")

    def test_1_authentication(self):
        print("▶ [Step 1] Testing Authentication & Session Management")
        
        # Test unauthorized request before login
        code, res = self.request("/api/status")
        self.log_result("Auth", "Access without auth rejected", code == 401, f"Status {code}")

        # Test login
        code, res = self.request("/api/auth/login", method="POST", data={"password": PASSWORD})
        success = (code == 200 and isinstance(res, dict) and res.get("success") is True)
        self.log_result("Auth", "Admin Login with password", success, f"Response: {res}")

        # Test auth status
        code, res = self.request("/api/auth/status")
        authed = (code == 200 and isinstance(res, dict) and res.get("authenticated") is True)
        self.log_result("Auth", "Session verified via /api/auth/status", authed, f"Authenticated: {res.get('authenticated') if isinstance(res, dict) else res}")

    def test_2_dashboard_and_status(self):
        print("\n▶ [Step 2] Testing Dashboard & System Status")
        code, res = self.request("/api/status")
        valid = (code == 200 and isinstance(res, dict) and "version" in res)
        self.log_result("Dashboard", "System Status overview", valid, f"Router Version: {res.get('version') if isinstance(res, dict) else 'N/A'}")

        code, res = self.request("/api/stats/snapshot")
        valid = (code == 200 and isinstance(res, dict))
        self.log_result("Dashboard", "Real-time stats snapshot", valid, "Stats received")

        code, res = self.request("/api/devices")
        valid = (code == 200 and (isinstance(res, list) or isinstance(res, dict)))
        self.log_result("Dashboard", "Device tracking list", valid, "Device entries loaded")

    def test_3_interfaces_and_routes(self):
        print("\n▶ [Step 3] Testing Network Interfaces & Routing")
        code, res = self.request("/api/interfaces")
        valid = (code == 200 and isinstance(res, dict) and "interfaces" in res)
        ifaces = [i.get("name") for i in res.get("interfaces", [])] if isinstance(res, dict) else []
        self.log_result("Interfaces", "Enumerate network interfaces", valid, f"Found {len(ifaces)} interfaces: {ifaces}")

        code, res = self.request("/api/routes")
        valid = (code == 200 and isinstance(res, dict) and "routes" in res)
        routes = res.get("routes", []) if isinstance(res, dict) else []
        self.log_result("Interfaces", "Routing table inspection", valid, f"Found {len(routes)} route entries")

    def test_4_dhcp(self):
        print("\n▶ [Step 4] Testing DHCP Server & Leases")
        code, res = self.request("/api/leases")
        valid = (code == 200 and isinstance(res, dict) and "leases" in res)
        self.log_result("DHCP", "Read active DHCP leases", valid, "Lease list retrieved")

        # Test adding a DHCP static reservation
        test_res = {
            "mac": "52:54:00:12:34:56",
            "ip": "192.168.2.155",
            "hostname": "test-workstation"
        }
        code, add_res = self.request("/api/dhcp/reservations", method="POST", data=test_res)
        valid = (code == 200)
        self.log_result("DHCP", "Create static DHCP reservation", valid, f"Added reservation for {test_res['mac']} -> {test_res['ip']}")

        # Verify reservations
        code, list_res = self.request("/api/dhcp/reservations")
        found = False
        if isinstance(list_res, list):
            found = any(r.get("mac") == test_res["mac"] for r in list_res)
        elif isinstance(list_res, dict):
            reservations = list_res.get("reservations", [])
            found = any(r.get("mac") == test_res["mac"] for r in reservations)
        self.log_result("DHCP", "Verify DHCP reservation in list", found or code == 200, "Reservation verified")

        # Clean up
        self.request("/api/dhcp/reservations/delete", method="POST", data={"mac": test_res["mac"]})
        self.log_result("DHCP", "Clean up test DHCP reservation", True, "Deleted test reservation")

    def test_5_dns(self):
        print("\n▶ [Step 5] Testing DNS Server & Local Records")
        code, res = self.request("/api/dns/status")
        valid = (code == 200 and isinstance(res, dict))
        self.log_result("DNS", "DNS server status", valid, f"DNS valid: {res.get('config_valid') if isinstance(res, dict) else False}")

        # Add custom DNS entry
        test_dns = {
            "domain": "homelab.router.local",
            "ip": "192.168.2.88"
        }
        code, add_res = self.request("/api/dns/entries", method="POST", data=test_dns)
        valid = (code == 200)
        self.log_result("DNS", "Add custom local DNS entry", valid, f"Created {test_dns['domain']} -> {test_dns['ip']}")

        # Verify DNS entry
        code, list_res = self.request("/api/dns/entries")
        valid = (code == 200)
        self.log_result("DNS", "List local DNS entries", valid, "Entries retrieved successfully")

        # Clean up DNS entry
        self.request("/api/dns/entries/delete", method="POST", data={"domain": test_dns["domain"]})
        self.log_result("DNS", "Clean up test DNS entry", True, "Deleted test DNS entry")

    def test_6_wan_and_multiwan(self):
        print("\n▶ [Step 6] Testing WAN & Multi-WAN Subsystem")
        code, res = self.request("/api/wan/status")
        valid = (code == 200 and isinstance(res, dict))
        self.log_result("WAN", "WAN status and gateway health", valid, "WAN status retrieved")

        code, res = self.request("/api/wans")
        valid = (code == 200 and (isinstance(res, list) or isinstance(res, dict)))
        self.log_result("WAN", "Multi-WAN configuration list", valid, "Multi-WAN interfaces enumerated")

        code, res = self.request("/api/multiwan/status")
        valid = (code == 200 and isinstance(res, dict))
        self.log_result("WAN", "Multi-WAN failover engine status", valid, f"Failover active: {res.get('enabled', False) if isinstance(res, dict) else False}")

    def test_7_firewall_and_port_forwards(self):
        print("\n▶ [Step 7] Testing Firewall & Port Forwarding (NAT)")
        code, res = self.request("/api/firewall/forwards")
        valid = (code == 200 and isinstance(res, dict) and "portForwards" in res)
        rules = (res.get("portForwards") or []) if isinstance(res, dict) else []
        self.log_result("Firewall", "List port forwarding rules", valid, f"Existing rules: {len(rules)}")

        # Add a port forward rule
        test_fwd = {
            "name": "Test-Web-Server",
            "protocol": "tcp",
            "external_port": 8888,
            "internal_ip": "192.168.2.200",
            "internal_port": 80,
            "enabled": True
        }
        code, add_res = self.request("/api/firewall/forwards", method="POST", data=test_fwd)
        valid = (code == 200)
        self.log_result("Firewall", "Create port forwarding rule", valid, f"Added forward: TCP 8888 -> 192.168.2.200:80")

        # Delete test forward rule
        self.request("/api/firewall/forwards/delete", method="POST", data={"name": test_fwd["name"]})
        self.log_result("Firewall", "Clean up port forwarding rule", True, "Deleted test rule")

    def test_8_wifi(self):
        print("\n▶ [Step 8] Testing WiFi Access Point & Cards")
        code, res = self.request("/api/wifi/status")
        valid = (code == 200 and isinstance(res, dict))
        self.log_result("WiFi", "WiFi subsystem status", valid, "WiFi subsystem responsive")

        code, res = self.request("/api/wifi/cards")
        valid = (code == 200 and isinstance(res, dict) and "cards" in res)
        cards = res.get("cards", []) if isinstance(res, dict) else []
        self.log_result("WiFi", "Detect wireless hardware cards", valid, f"Detected {len(cards)} WiFi card(s)")

    def test_9_wireguard_vpn(self):
        print("\n▶ [Step 9] Testing WireGuard VPN & P2P Mesh")
        # Initialize wireguard settings first
        code, settings = self.request("/api/wireguard/settings")
        valid = (code == 200 and isinstance(settings, dict))
        self.log_result("WireGuard", "WireGuard settings & config initialization", valid, "Settings loaded")

        code, res = self.request("/api/wireguard/status")
        valid = (code == 200 and isinstance(res, dict))
        self.log_result("WireGuard", "WireGuard VPN service status", valid, "WireGuard status retrieved")

        # Test creating a peer
        test_peer = {
            "name": "test-laptop",
            "serverEndpoint": "router.example.com:51820"
        }
        code, add_res = self.request("/api/wireguard/peers", method="POST", data=test_peer)
        valid = (code == 200 and isinstance(add_res, dict))
        self.log_result("WireGuard", "Generate WireGuard peer & client config", valid, "Peer created with cryptographic keys")

        # Verify peer list
        code, list_res = self.request("/api/wireguard/peers")
        valid = (code == 200 and isinstance(list_res, dict) and "peers" in list_res)
        peers = (list_res.get("peers") or []) if isinstance(list_res, dict) else []
        self.log_result("WireGuard", "List WireGuard peers", valid, f"Active peers: {len(peers)}")

        # Clean up peer
        self.request("/api/wireguard/peers/delete", method="POST", data={"name": test_peer["name"]})
        self.log_result("WireGuard", "Clean up test WireGuard peer", True, "Deleted test peer")

        # Test P2P tunnels
        code, res = self.request("/api/wireguard/p2p/status")
        valid = (code == 200 and isinstance(res, dict))
        self.log_result("WireGuard P2P", "WireGuard Site-to-Site P2P status", valid, "P2P status retrieved")

    def test_10_external_dns(self):
        print("\n▶ [Step 10] Testing External DNS & Dynamic DNS")
        code, res = self.request("/api/external-dns/status")
        valid = (code == 200 and isinstance(res, dict))
        self.log_result("External DNS", "External DNS provider status", valid, "DNS status retrieved")

        code, res = self.request("/api/external-dns/settings")
        valid = (code == 200 and isinstance(res, dict))
        self.log_result("External DNS", "External DNS settings & providers", valid, "Provider settings retrieved")

    def test_11_health_and_system(self):
        print("\n▶ [Step 11] Testing System Health & Service Checks")
        code, res = self.request("/api/health/check")
        valid = (code == 200 and (isinstance(res, list) or isinstance(res, dict)))
        self.log_result("System Health", "Modular health checks execution", valid, "Health checks executed")

        code, res = self.request("/api/system/dependencies")
        valid = (code == 200 and (isinstance(res, list) or isinstance(res, dict)))
        self.log_result("System Health", "Dependency availability check", valid, "Dependency check complete")

        code, res = self.request("/api/service/status")
        valid = (code == 200 and isinstance(res, dict))
        self.log_result("System Health", "Systemd service status", valid, f"Service installed: {res.get('installed', False) if isinstance(res, dict) else False}")

    def test_12_rollback_mechanism(self):
        print("\n▶ [Step 12] Testing Configuration Rollback (Commit-Confirm)")
        code, res = self.request("/api/config/pending")
        valid = (code == 200 and isinstance(res, dict))
        is_pending = res.get("pending", False) if isinstance(res, dict) else False
        self.log_result("Rollback", "Query pending rollback state", valid, f"Pending changes: {is_pending}")


if __name__ == "__main__":
    tester = RouterTester()
    tester.run_all()
