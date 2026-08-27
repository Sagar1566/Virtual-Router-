package server

import (
	"html/template"
)

func (s *Server) parseTemplates() {
	funcMap := template.FuncMap{
		"join": func(sep string, items []string) string {
			result := ""
			for i, item := range items {
				if i > 0 {
					result += sep
				}
				result += item
			}
			return result
		},
	}

	s.templates = template.New("").Funcs(funcMap)

	// Login is standalone
	template.Must(s.templates.New("login").Parse(loginTemplate))

	// All other pages use the page layout
	template.Must(s.templates.New("admin").Parse(pageLayout(adminContent)))
	template.Must(s.templates.New("setup").Parse(pageLayout(setupContent)))
	template.Must(s.templates.New("interfaces").Parse(pageLayout(interfacesContent)))
	template.Must(s.templates.New("dns").Parse(pageLayout(dnsContent)))
	template.Must(s.templates.New("dhcp").Parse(pageLayout(dhcpContent)))
	template.Must(s.templates.New("wireguard").Parse(pageLayout(wireguardContent)))
	template.Must(s.templates.New("wifi").Parse(pageLayout(wifiContent)))
	template.Must(s.templates.New("firewall").Parse(pageLayout(firewallContent)))
	template.Must(s.templates.New("settings").Parse(pageLayout(settingsContent)))
}

// pageLayout wraps content in the standard page layout
func pageLayout(content string) string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Ubuntu Router</title>
    <style>
        :root {
            --bg: #1a1a2e;
            --bg-light: #16213e;
            --bg-card: #0f3460;
            --text: #eaeaea;
            --text-muted: #a0a0a0;
            --primary: #e94560;
            --primary-hover: #ff6b6b;
            --success: #00d9a5;
            --warning: #ffc107;
            --error: #dc3545;
            --border: #333;
        }
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: var(--bg);
            color: var(--text);
            line-height: 1.6;
        }
        .container { max-width: 1200px; margin: 0 auto; padding: 20px; }
        header {
            background: var(--bg-light);
            padding: 15px 20px;
            border-bottom: 1px solid var(--border);
            display: flex;
            justify-content: space-between;
            align-items: center;
        }
        header h1 { font-size: 1.5rem; }
        header nav a {
            color: var(--text-muted);
            text-decoration: none;
            margin-left: 20px;
            transition: color 0.2s;
        }
        header nav a:hover { color: var(--primary); }
        .card {
            background: var(--bg-card);
            border-radius: 8px;
            padding: 20px;
            margin-bottom: 20px;
        }
        .card h2 {
            font-size: 1.2rem;
            margin-bottom: 15px;
            color: var(--primary);
        }
        .grid { display: grid; gap: 20px; }
        .grid-2 { grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); }
        .grid-3 { grid-template-columns: repeat(auto-fit, minmax(250px, 1fr)); }
        table { width: 100%; border-collapse: collapse; }
        th, td {
            padding: 10px;
            text-align: left;
            border-bottom: 1px solid var(--border);
        }
        th { color: var(--text-muted); font-weight: 500; }
        .btn {
            display: inline-block;
            padding: 8px 16px;
            background: var(--primary);
            color: white;
            border: none;
            border-radius: 4px;
            cursor: pointer;
            text-decoration: none;
            font-size: 0.9rem;
        }
        .btn:hover { background: var(--primary-hover); }
        .btn-sm { padding: 4px 8px; font-size: 0.8rem; }
        .btn-success { background: var(--success); }
        .btn-warning { background: var(--warning); color: #000; }
        .btn-danger { background: var(--error); }
        .btn-secondary { background: var(--bg-light); }
        input, select { padding: 10px; background: var(--bg); border: 1px solid var(--border); border-radius: 4px; color: var(--text); }
        input:focus, select:focus { outline: none; border-color: var(--primary); }
        label { display: block; margin-bottom: 5px; color: var(--text-muted); }
        .form-group { margin-bottom: 15px; }
        .form-inline { display: flex; gap: 10px; align-items: flex-end; }
        .form-inline .form-group { margin-bottom: 0; flex: 1; }
        .alert { padding: 12px 15px; border-radius: 4px; margin-bottom: 20px; }
        .alert-success { background: rgba(0, 217, 165, 0.2); border: 1px solid var(--success); }
        .alert-error { background: rgba(220, 53, 69, 0.2); border: 1px solid var(--error); }
        .status-dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 6px; }
        .status-up { background: var(--success); }
        .status-down { background: var(--error); }
        .badge { display: inline-block; padding: 2px 8px; border-radius: 12px; font-size: 0.75rem; }
        .badge-success { background: var(--success); color: #000; }
        .badge-error { background: var(--error); }
        code { background: var(--bg); padding: 2px 6px; border-radius: 3px; font-family: monospace; }
        .text-muted { color: var(--text-muted); }
        .mb-2 { margin-bottom: 10px; }
        .mb-4 { margin-bottom: 20px; }
        .mt-2 { margin-top: 10px; }
        .mt-4 { margin-top: 20px; }
        .flex { display: flex; }
        .gap-2 { gap: 10px; }
        .dry-run-banner { background: var(--warning); color: #000; padding: 8px; text-align: center; }
    </style>
</head>
<body>
    {{if .DryRun}}<div class="dry-run-banner">DRY RUN MODE - No changes will be applied</div>{{end}}
    <header>
        <h1>Ubuntu Router</h1>
        <nav>
            <a href="/admin">Dashboard</a>
            <a href="/admin/interfaces">Interfaces</a>
            <a href="/admin/dns">DNS</a>
            <a href="/admin/dhcp">DHCP</a>
            <a href="/admin/wireguard">WireGuard</a>
            <a href="/admin/wifi">WiFi</a>
            <a href="/admin/firewall">Firewall</a>
            <a href="/admin/settings">Settings</a>
            <a href="/logout">Logout</a>
        </nav>
    </header>
    <div class="container">` + content + `</div>
</body>
</html>`
}

const loginTemplate = `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Ubuntu Router - Login</title>
    <style>
        :root { --bg: #1a1a2e; --bg-card: #0f3460; --text: #eaeaea; --primary: #e94560; --error: #dc3545; --border: #333; }
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: var(--bg); color: var(--text); min-height: 100vh; display: flex; align-items: center; justify-content: center; }
        .login-box { background: var(--bg-card); padding: 40px; border-radius: 8px; width: 100%; max-width: 400px; }
        h1 { text-align: center; margin-bottom: 30px; }
        .form-group { margin-bottom: 20px; }
        label { display: block; margin-bottom: 8px; color: #a0a0a0; }
        input { width: 100%; padding: 12px; background: var(--bg); border: 1px solid var(--border); border-radius: 4px; color: var(--text); font-size: 1rem; }
        input:focus { outline: none; border-color: var(--primary); }
        button { width: 100%; padding: 12px; background: var(--primary); color: white; border: none; border-radius: 4px; font-size: 1rem; cursor: pointer; }
        .error { background: rgba(220, 53, 69, 0.2); border: 1px solid var(--error); padding: 10px; border-radius: 4px; margin-bottom: 20px; text-align: center; }
    </style>
</head>
<body>
    <div class="login-box">
        <h1>Ubuntu Router</h1>
        {{if .Data.Error}}<div class="error">Invalid password</div>{{end}}
        <form method="POST" action="/login">
            <input type="hidden" name="next" value="{{.Data.Next}}">
            <div class="form-group">
                <label>Password</label>
                <input type="password" name="password" placeholder="Enter password" autofocus required>
            </div>
            <button type="submit">Login</button>
        </form>
    </div>
</body>
</html>`

const adminContent = `{{with .Data}}
{{if .Message}}<div class="alert alert-success">{{.Message}}</div>{{end}}
{{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}
<div class="grid grid-3">
    <div class="card">
        <h2>System Status</h2>
        <table>
            <tr><td>IP Forwarding</td><td>{{if .IPForwardEnabled}}<span class="badge badge-success">Enabled</span>{{else}}<span class="badge badge-error">Disabled</span>{{end}}</td></tr>
            <tr><td>WAN Interface</td><td><code>{{.Config.WANInterface}}</code></td></tr>
            <tr><td>Default Route</td><td><code>{{.DefaultInterface}}</code></td></tr>
        </table>
    </div>
    <div class="card">
        <h2>DNS/DHCP Status</h2>
        {{if .DNSStatus}}
        <table>
            <tr><td>Service</td><td>{{if .DNSStatus.Running}}<span class="badge badge-success">Running</span>{{else}}<span class="badge badge-error">Stopped</span>{{end}}</td></tr>
            <tr><td>Active Leases</td><td>{{.DNSStatus.LeaseCount}}</td></tr>
        </table>
        {{else}}<p class="text-muted">Not configured</p>{{end}}
    </div>
    <div class="card">
        <h2>WireGuard Status</h2>
        {{if .WireGuardStatus}}
        <table>
            <tr><td>Service</td><td>{{if .WireGuardStatus.Running}}<span class="badge badge-success">Running</span>{{else}}<span class="badge badge-error">Stopped</span>{{end}}</td></tr>
            <tr><td>Peers</td><td>{{.WireGuardStatus.ActivePeers}} / {{.WireGuardStatus.PeerCount}}</td></tr>
        </table>
        {{else}}<p class="text-muted">Not configured</p>{{end}}
    </div>
</div>
<div class="card">
    <h2>Network Interfaces</h2>
    <table>
        <thead><tr><th>Name</th><th>Type</th><th>Status</th><th>MAC</th><th>IP Addresses</th></tr></thead>
        <tbody>
        {{range .Interfaces}}
        <tr>
            <td><code>{{.Name}}</code></td>
            <td>{{.Type}}</td>
            <td>{{if eq .State "up"}}<span class="status-dot status-up"></span>Up{{else}}<span class="status-dot status-down"></span>Down{{end}}</td>
            <td><code>{{.MAC}}</code></td>
            <td>{{range .Addresses}}<code>{{.IP}}/{{.Prefix}}</code> {{end}}</td>
        </tr>
        {{end}}
        </tbody>
    </table>
</div>
<div class="card">
    <h2>Quick Actions</h2>
    <div class="flex gap-2">
        <a href="/admin/setup" class="btn">Run Setup Wizard</a>
        <a href="/admin/interfaces" class="btn btn-secondary">Manage Interfaces</a>
    </div>
</div>
{{end}}`

const setupContent = `{{with .Data}}
<div class="card">
    <h2>Setup Wizard</h2>
    {{if eq .Step ""}}
    <p class="mb-4">This wizard will help you configure your router.</p>
    <a href="/admin/setup?step=wan" class="btn">Start Setup</a>
    {{else if eq .Step "wan"}}
    <h3 class="mb-2">Step 1: WAN Configuration</h3>
    <form method="POST">
        <input type="hidden" name="step" value="wan">
        <div class="form-group"><label>WAN Interface</label>
            <select name="wan_interface">{{range .Interfaces}}<option value="{{.Name}}">{{.Name}}</option>{{end}}</select>
        </div>
        <div class="form-group"><label>WAN Mode</label>
            <select name="wan_mode"><option value="dhcp">DHCP</option><option value="static">Static</option></select>
        </div>
        <button type="submit" class="btn">Next</button>
    </form>
    {{else if eq .Step "lan"}}
    <h3 class="mb-2">Step 2: LAN Configuration</h3>
    <form method="POST">
        <input type="hidden" name="step" value="lan">
        <div class="form-group"><label>Bridge Name</label><input type="text" name="lan_bridge" value="br0"></div>
        <div class="form-group"><label>LAN Address (CIDR)</label><input type="text" name="lan_address" placeholder="192.168.1.1/24"></div>
        <button type="submit" class="btn">Next</button>
    </form>
    {{else if eq .Step "dhcp"}}
    <h3 class="mb-2">Step 3: DHCP Configuration</h3>
    <form method="POST">
        <input type="hidden" name="step" value="dhcp">
        <div class="form-group"><label><input type="checkbox" name="dhcp_enabled" checked> Enable DHCP</label></div>
        <div class="form-group"><label>DHCP Start</label><input type="text" name="dhcp_start" placeholder="192.168.1.100"></div>
        <div class="form-group"><label>DHCP End</label><input type="text" name="dhcp_end" placeholder="192.168.1.200"></div>
        <button type="submit" class="btn">Next</button>
    </form>
    {{else if eq .Step "dns"}}
    <h3 class="mb-2">Step 4: DNS Configuration</h3>
    <form method="POST">
        <input type="hidden" name="step" value="dns">
        <div class="form-group"><label><input type="checkbox" name="dns_enabled" checked> Enable DNS</label></div>
        <div class="form-group"><label>Upstream DNS</label><input type="text" name="dns_upstream" value="8.8.8.8, 1.1.1.1"></div>
        <button type="submit" class="btn">Next</button>
    </form>
    {{else if eq .Step "apply"}}
    <h3 class="mb-2">Step 5: Apply Configuration</h3>
    <form method="POST"><input type="hidden" name="step" value="apply"><button type="submit" class="btn btn-success">Apply Configuration</button></form>
    {{end}}
</div>
{{end}}`

const interfacesContent = `{{with .Data}}
{{if .Message}}<div class="alert alert-success">{{.Message}}</div>{{end}}
{{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}
<div class="card">
    <h2>Network Interfaces</h2>
    <table>
        <thead><tr><th>Name</th><th>Type</th><th>Status</th><th>Link</th><th>MAC</th><th>IP</th><th>Actions</th></tr></thead>
        <tbody>
        {{range .Interfaces}}
        <tr>
            <td><code>{{.Name}}</code></td>
            <td>{{.Type}}</td>
            <td>{{if eq .State "up"}}<span class="badge badge-success">Up</span>{{else}}<span class="badge badge-error">Down</span>{{end}}</td>
            <td>{{if eq .LinkState "up"}}Connected{{else}}No carrier{{end}}</td>
            <td><code>{{.MAC}}</code></td>
            <td>{{range .Addresses}}<code>{{.IP}}/{{.Prefix}}</code> {{end}}</td>
            <td>
                <form method="POST" action="/admin/interfaces/configure" style="display:inline">
                    <input type="hidden" name="interface" value="{{.Name}}">
                    {{if eq .State "up"}}<button name="action" value="down" class="btn btn-sm btn-secondary">Down</button>
                    {{else}}<button name="action" value="up" class="btn btn-sm btn-success">Up</button>{{end}}
                </form>
            </td>
        </tr>
        {{end}}
        </tbody>
    </table>
</div>
<div class="card">
    <h2>Routing Table</h2>
    <table>
        <thead><tr><th>Destination</th><th>Gateway</th><th>Interface</th><th>Metric</th></tr></thead>
        <tbody>
        {{range .Routes}}<tr><td><code>{{.Destination}}</code></td><td>{{if .Gateway}}<code>{{.Gateway}}</code>{{else}}-{{end}}</td><td><code>{{.Interface}}</code></td><td>{{.Metric}}</td></tr>{{end}}
        </tbody>
    </table>
</div>
{{end}}`

const dnsContent = `{{with .Data}}
{{if .Message}}<div class="alert alert-success">{{.Message}}</div>{{end}}
{{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}
<div class="grid grid-2">
    <div class="card">
        <h2>DNS Status</h2>
        {{if .Status}}<table>
            <tr><td>Service</td><td>{{if .Status.Running}}<span class="badge badge-success">Running</span>{{else}}<span class="badge badge-error">Stopped</span>{{end}}</td></tr>
            <tr><td>Upstream</td><td>{{range .Status.UpstreamDNS}}<code>{{.}}</code> {{end}}</td></tr>
        </table>{{end}}
    </div>
    <div class="card">
        <h2>Add DNS Entry</h2>
        <form method="POST" action="/admin/dns/add">
            <div class="form-inline">
                <div class="form-group"><label>Hostname</label><input type="text" name="hostname" required></div>
                <div class="form-group"><label>IP</label><input type="text" name="ip" required></div>
                <button type="submit" class="btn">Add</button>
            </div>
        </form>
    </div>
</div>
<div class="card">
    <h2>DNS Entries</h2>
    <table>
        <thead><tr><th>Hostname</th><th>IP</th><th>Actions</th></tr></thead>
        <tbody>
        {{range .Entries}}<tr><td><code>{{.Hostname}}</code></td><td><code>{{.IP}}</code></td><td>
            <form method="POST" action="/admin/dns/remove" style="display:inline"><input type="hidden" name="hostname" value="{{.Hostname}}"><button class="btn btn-sm btn-danger">Remove</button></form>
        </td></tr>{{else}}<tr><td colspan="3" class="text-muted">No entries</td></tr>{{end}}
        </tbody>
    </table>
</div>
{{end}}`

const dhcpContent = `{{with .Data}}
{{if .Message}}<div class="alert alert-success">{{.Message}}</div>{{end}}
{{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}
<div class="card">
    <h2>Add Reservation</h2>
    <form method="POST" action="/admin/dhcp/reservation/add">
        <div class="form-inline">
            <div class="form-group"><label>Name</label><input type="text" name="name" required></div>
            <div class="form-group"><label>MAC</label><input type="text" name="mac" required></div>
            <div class="form-group"><label>IP</label><input type="text" name="ip" required></div>
            <button type="submit" class="btn">Add</button>
        </div>
    </form>
</div>
<div class="grid grid-2">
    <div class="card">
        <h2>Reservations</h2>
        <table>
            <thead><tr><th>Name</th><th>MAC</th><th>IP</th><th>Actions</th></tr></thead>
            <tbody>
            {{range .Reservations}}<tr><td>{{.Name}}</td><td><code>{{.MAC}}</code></td><td><code>{{.IP}}</code></td><td>
                <form method="POST" action="/admin/dhcp/reservation/remove" style="display:inline"><input type="hidden" name="mac" value="{{.MAC}}"><button class="btn btn-sm btn-danger">Remove</button></form>
            </td></tr>{{else}}<tr><td colspan="4" class="text-muted">No reservations</td></tr>{{end}}
            </tbody>
        </table>
    </div>
    <div class="card">
        <h2>Active Leases</h2>
        <table>
            <thead><tr><th>Hostname</th><th>IP</th><th>MAC</th></tr></thead>
            <tbody>
            {{range .Leases}}<tr><td>{{.Hostname}}</td><td><code>{{.IP}}</code></td><td><code>{{.MAC}}</code></td></tr>{{else}}<tr><td colspan="3" class="text-muted">No leases</td></tr>{{end}}
            </tbody>
        </table>
    </div>
</div>
{{end}}`

const wireguardContent = `{{with .Data}}
{{if .Message}}<div class="alert alert-success">{{.Message}}</div>{{end}}
{{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}
<div class="grid grid-2">
    <div class="card">
        <h2>WireGuard Status</h2>
        {{if .Status}}<table>
            <tr><td>Interface</td><td><code>{{.Status.Interface}}</code></td></tr>
            <tr><td>Status</td><td>{{if .Status.Running}}<span class="badge badge-success">Running</span>{{else}}<span class="badge badge-error">Stopped</span>{{end}}</td></tr>
            <tr><td>Port</td><td>{{.Status.ListenPort}}</td></tr>
            <tr><td>Peers</td><td>{{.Status.ActivePeers}} / {{.Status.PeerCount}}</td></tr>
        </table>
        <div class="mt-4">
            <form method="POST" action="/admin/wireguard/toggle" style="display:inline">
                {{if .Status.Running}}<button name="action" value="stop" class="btn btn-danger">Stop</button><button name="action" value="restart" class="btn btn-warning">Restart</button>
                {{else}}<button name="action" value="start" class="btn btn-success">Start</button>{{end}}
            </form>
        </div>
        {{else}}<p class="text-muted">Not configured</p>{{end}}
    </div>
    <div class="card">
        <h2>Add Peer</h2>
        <form method="POST" action="/admin/wireguard/peer/add">
            <div class="form-group"><label>Name</label><input type="text" name="name" required></div>
            <div class="form-group"><label>Server Endpoint</label><input type="text" name="endpoint" placeholder="vpn.example.com" required></div>
            <button type="submit" class="btn">Generate Config</button>
        </form>
    </div>
</div>
{{if .Config}}<div class="card">
    <h2>Peers</h2>
    <table>
        <thead><tr><th>Name</th><th>Public Key</th><th>Allowed IPs</th><th>Actions</th></tr></thead>
        <tbody>
        {{range .Config.Peers}}<tr><td>{{.Name}}</td><td><code>{{.PublicKey}}</code></td><td>{{range .AllowedIPs}}<code>{{.}}</code> {{end}}</td><td>
            <form method="POST" action="/admin/wireguard/peer/remove" style="display:inline"><input type="hidden" name="public_key" value="{{.PublicKey}}"><button class="btn btn-sm btn-danger">Remove</button></form>
        </td></tr>{{else}}<tr><td colspan="4" class="text-muted">No peers</td></tr>{{end}}
        </tbody>
    </table>
</div>{{end}}
{{end}}`

const wifiContent = `{{with .Data}}
{{if .Message}}<div class="alert alert-success">{{.Message}}</div>{{end}}
{{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}
<div class="card">
    <h2>Configure AP</h2>
    <form method="POST" action="/admin/wifi/configure">
        <div class="grid grid-2">
            <div class="form-group"><label>Interface</label><select name="interface">{{range .AvailableIfaces}}<option>{{.}}</option>{{end}}</select></div>
            <div class="form-group"><label>SSID</label><input type="text" name="ssid" required></div>
            <div class="form-group"><label>Password</label><input type="password" name="password" minlength="8"></div>
            <div class="form-group"><label>Channel</label><input type="number" name="channel" value="6"></div>
            <div class="form-group"><label>Band</label><select name="band"><option value="2.4ghz">2.4 GHz</option><option value="5ghz">5 GHz</option></select></div>
            <div class="form-group"><label>Bridge</label><input type="text" name="bridge" value="br0"></div>
        </div>
        <button type="submit" class="btn">Save & Apply</button>
    </form>
</div>
{{if .Status}}<div class="card">
    <h2>Active Interfaces</h2>
    {{range .Status.Interfaces}}<div class="mb-4">
        <h3><code>{{.Interface}}</code> {{if .Running}}<span class="badge badge-success">Running</span>{{else}}<span class="badge badge-error">Stopped</span>{{end}}</h3>
        <table><tr><td>SSID</td><td>{{.SSID}}</td></tr><tr><td>Channel</td><td>{{.Channel}}</td></tr></table>
        <form method="POST" action="/admin/wifi/toggle" style="display:inline" class="mt-2">
            <input type="hidden" name="interface" value="{{.Interface}}">
            {{if .Running}}<button name="action" value="stop" class="btn btn-sm btn-danger">Stop</button>
            {{else}}<button name="action" value="start" class="btn btn-sm btn-success">Start</button>{{end}}
        </form>
    </div>{{end}}
</div>{{end}}
{{end}}`

const firewallContent = `{{with .Data}}
{{if .Message}}<div class="alert alert-success">{{.Message}}</div>{{end}}
{{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}
<div class="card">
    <h2>Add Port Forward</h2>
    <form method="POST" action="/admin/firewall/forward/add">
        <div class="grid grid-3">
            <div class="form-group"><label>Name</label><input type="text" name="name" required></div>
            <div class="form-group"><label>Protocol</label><select name="protocol"><option value="tcp">TCP</option><option value="udp">UDP</option></select></div>
            <div class="form-group"><label>External Port</label><input type="number" name="external_port" required></div>
            <div class="form-group"><label>Internal IP</label><input type="text" name="internal_ip" required></div>
            <div class="form-group"><label>Internal Port</label><input type="number" name="internal_port" required></div>
            <div class="form-group"><label><input type="checkbox" name="enabled" checked> Enabled</label></div>
        </div>
        <button type="submit" class="btn">Add</button>
    </form>
</div>
<div class="card">
    <h2>Port Forwards</h2>
    <table>
        <thead><tr><th>Name</th><th>Protocol</th><th>External</th><th>Internal</th><th>Status</th><th>Actions</th></tr></thead>
        <tbody>
        {{range .PortForwards}}<tr><td>{{.Name}}</td><td>{{.Protocol}}</td><td>{{.ExternalPort}}</td><td><code>{{.InternalIP}}:{{.InternalPort}}</code></td><td>{{if .Enabled}}<span class="badge badge-success">Active</span>{{else}}<span class="badge badge-error">Disabled</span>{{end}}</td><td>
            <form method="POST" action="/admin/firewall/forward/remove" style="display:inline"><input type="hidden" name="name" value="{{.Name}}"><button class="btn btn-sm btn-danger">Remove</button></form>
        </td></tr>{{else}}<tr><td colspan="6" class="text-muted">No forwards</td></tr>{{end}}
        </tbody>
    </table>
</div>
{{end}}`

const settingsContent = `{{with .Data}}
{{if .Message}}<div class="alert alert-success">{{.Message}}</div>{{end}}
{{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}
<div class="card">
    <h2>General Settings</h2>
    <form method="POST" action="/admin/settings/save">
        <div class="form-group"><label>Listen Address</label><input type="text" name="listen_addr" value="{{.Config.ListenAddr}}"></div>
        <button type="submit" class="btn">Save</button>
    </form>
</div>
<div class="card">
    <h2>Actions</h2>
    <a href="/admin/setup" class="btn btn-secondary">Run Setup Wizard</a>
</div>
{{end}}`
