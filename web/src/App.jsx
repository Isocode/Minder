import React, { useEffect, useState } from 'react';

// Utility to call the backend API with credentials.  Returns JSON or throws.
async function api(path, opts = {}) {
  const res = await fetch(path, {
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      ...(opts.headers || {})
    },
    ...opts
  });
  if (!res.ok) {
    const msg = await res.text();
    throw new Error(msg || res.statusText);
  }
  return res.status === 204 ? null : res.json();
}

export default function App() {
  const [loggedIn, setLoggedIn] = useState(false);
  const [loginError, setLoginError] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [status, setStatus] = useState(null);
  const [zones, setZones] = useState([]);
  const [users, setUsers] = useState([]);
  const [armModes, setArmModes] = useState([]);
  const [currentMode, setCurrentMode] = useState('');
  const [page, setPage] = useState('status');
  const [newZone, setNewZone] = useState({ name: '', type: 'contact', pin: '', enabled: true });
  const [zoneError, setZoneError] = useState('');
  const [armModeName, setArmModeName] = useState('');
  const [newArmModeZones, setNewArmModeZones] = useState('');
  const [newUser, setNewUser] = useState({ username: '', password: '', admin: false });
  const [userError, setUserError] = useState('');

  // Load status periodically
  useEffect(() => {
    if (!loggedIn) return;
    async function load() {
      try {
        const data = await api('/api/status');
        setStatus(data);
        setCurrentMode(data.mode);
        setZones(data.zones);
      } catch (err) {
        console.error(err);
      }
    }
    load();
    const id = setInterval(load, 5000);
    return () => clearInterval(id);
  }, [loggedIn]);

  // Fetch initial data for zones, users, arm modes when logged in
  useEffect(() => {
    if (!loggedIn) return;
    async function loadAll() {
      try {
        const zs = await api('/api/zones');
        setZones(zs);
        const us = await api('/api/users');
        setUsers(us);
        const ams = await api('/api/arm_modes');
        setArmModes(ams);
      } catch (err) {
        console.error(err);
      }
    }
    loadAll();
  }, [loggedIn]);

  async function handleLogin(e) {
    e.preventDefault();
    try {
      await api('/api/login', {
        method: 'POST',
        body: JSON.stringify({ username, password })
      });
      setLoggedIn(true);
      setLoginError('');
    } catch (err) {
      setLoginError('Login failed');
    }
  }

  async function handleLogout() {
    await api('/api/logout', { method: 'POST' });
    setLoggedIn(false);
    setUsername('');
    setPassword('');
  }

  async function armSystem(mode) {
    await api('/api/arm', { method: 'POST', body: JSON.stringify({ mode }) });
    setCurrentMode(mode);
  }

  async function disarmSystem() {
    await api('/api/disarm', { method: 'POST' });
    setCurrentMode('Disarmed');
  }

  async function createZone() {
    try {
      const pinNum = parseInt(newZone.pin, 10);
      if (isNaN(pinNum)) throw new Error('Pin must be a number');
      const zone = { ...newZone, pin: pinNum, enabled: !!newZone.enabled };
      await api('/api/zones', { method: 'POST', body: JSON.stringify(zone) });
      setNewZone({ name: '', type: 'contact', pin: '', enabled: true });
      setZoneError('');
      const zs = await api('/api/zones');
      setZones(zs);
    } catch (err) {
      setZoneError(err.message);
    }
  }

  async function deleteZone(id) {
    await api(`/api/zones/${id}`, { method: 'DELETE' });
    const zs = await api('/api/zones');
    setZones(zs);
  }

  async function createArmMode() {
    try {
      const ids = newArmModeZones
        .split(',')
        .map((s) => s.trim())
        .filter((s) => s.length > 0)
        .map((s) => parseInt(s, 10))
        .filter((n) => !isNaN(n));
      const mode = { name: armModeName, active_zones: ids };
      await api('/api/arm_modes', { method: 'POST', body: JSON.stringify(mode) });
      setArmModeName('');
      setNewArmModeZones('');
      const ams = await api('/api/arm_modes');
      setArmModes(ams);
    } catch (err) {
      console.error(err);
    }
  }

  async function createUser() {
    try {
      await api('/api/users', { method: 'POST', body: JSON.stringify(newUser) });
      setNewUser({ username: '', password: '', admin: false });
      const us = await api('/api/users');
      setUsers(us);
      setUserError('');
    } catch (err) {
      setUserError(err.message);
    }
  }

  async function deleteUser(name) {
    await api(`/api/users/${name}`, { method: 'DELETE' });
    const us = await api('/api/users');
    setUsers(us);
  }

  if (!loggedIn) {
    return (
      <div className="login-container">
        <h2>Login to Minder</h2>
        <form onSubmit={handleLogin} className="card">
          <label>
            Username
            <input value={username} onChange={(e) => setUsername(e.target.value)} required />
          </label>
          <label>
            Password
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          </label>
          {loginError && <p className="error">{loginError}</p>}
          <button type="submit">Login</button>
        </form>
      </div>
    );
  }

  return (
    <div className="app-container">
      <header>
        <h1>Minder Alarm</h1>
        <nav>
          <button onClick={() => setPage('status')} className={page === 'status' ? 'active' : ''}>Status</button>
          <button onClick={() => setPage('zones')} className={page === 'zones' ? 'active' : ''}>Zones</button>
          <button onClick={() => setPage('armModes')} className={page === 'armModes' ? 'active' : ''}>Arm Modes</button>
          <button onClick={() => setPage('users')} className={page === 'users' ? 'active' : ''}>Users</button>
          <button onClick={handleLogout}>Logout</button>
        </nav>
      </header>
      <main>
        {page === 'status' && status && (
          <div className="status">
            <div className="card">
              <h2>System Status</h2>
              <p>Mode: <strong>{currentMode}</strong></p>
              <div className="buttons">
                <button onClick={() => armSystem('Away')} disabled={currentMode === 'Away'}>Arm Away</button>
                <button onClick={() => armSystem('Home')} disabled={currentMode === 'Home'}>Arm Home</button>
                <button onClick={disarmSystem} disabled={currentMode === 'Disarmed'}>Disarm</button>
              </div>
              <h3>Zones</h3>
              <table>
                <thead>
                  <tr><th>ID</th><th>Name</th><th>Type</th><th>Pin</th><th>Enabled</th><th>Triggered</th></tr>
                </thead>
                <tbody>
                  {zones.map((z) => (
                    <tr key={z.id} className={z.active ? 'triggered' : ''}>
                      <td>{z.id}</td>
                      <td>{z.name}</td>
                      <td>{z.type}</td>
                      <td>{z.pin}</td>
                      <td>{z.enabled ? 'Yes' : 'No'}</td>
                      <td>{z.active ? 'Yes' : 'No'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {page === 'zones' && (
          <div className="zones">
            <div className="card">
              <h2>Zones</h2>
              <table>
                <thead>
                  <tr><th>ID</th><th>Name</th><th>Type</th><th>Pin</th><th>Enabled</th><th>Actions</th></tr>
                </thead>
                <tbody>
                  {zones.map((z) => (
                    <tr key={z.id}>
                      <td>{z.id}</td>
                      <td>{z.name}</td>
                      <td>{z.type}</td>
                      <td>{z.pin}</td>
                      <td>{z.enabled ? 'Yes' : 'No'}</td>
                      <td><button onClick={() => deleteZone(z.id)}>Delete</button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <h3>Add Zone</h3>
              <div className="form-row">
                <input placeholder="Name" value={newZone.name} onChange={(e) => setNewZone({ ...newZone, name: e.target.value })} />
                <select value={newZone.type} onChange={(e) => setNewZone({ ...newZone, type: e.target.value })}>
                  <option value="contact">Contact</option>
                  <option value="pir">PIR</option>
                </select>
                <input placeholder="Pin" value={newZone.pin} onChange={(e) => setNewZone({ ...newZone, pin: e.target.value })} />
                <label><input type="checkbox" checked={newZone.enabled} onChange={(e) => setNewZone({ ...newZone, enabled: e.target.checked })} /> Enabled</label>
                <button onClick={createZone}>Create</button>
              </div>
              {zoneError && <p className="error">{zoneError}</p>}
            </div>
          </div>
        )}

        {page === 'armModes' && (
          <div className="armmodes">
            <div className="card">
              <h2>Arm Modes</h2>
              <table>
                <thead>
                  <tr><th>Name</th><th>Active Zones</th></tr>
                </thead>
                <tbody>
                  {armModes.map((am) => (
                    <tr key={am.name}>
                      <td>{am.name}</td>
                      <td>{am.active_zones.join(', ')}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <h3>Add/Update Arm Mode</h3>
              <div className="form-row">
                <input placeholder="Mode Name" value={armModeName} onChange={(e) => setArmModeName(e.target.value)} />
                <input placeholder="Zone IDs (comma separated)" value={newArmModeZones} onChange={(e) => setNewArmModeZones(e.target.value)} />
                <button onClick={createArmMode}>Save</button>
              </div>
            </div>
          </div>
        )}

        {page === 'users' && (
          <div className="users">
            <div className="card">
              <h2>Users</h2>
              <table>
                <thead>
                  <tr><th>Username</th><th>Admin</th><th>Actions</th></tr>
                </thead>
                <tbody>
                  {users.map((u) => (
                    <tr key={u.username}>
                      <td>{u.username}</td>
                      <td>{u.admin ? 'Yes' : 'No'}</td>
                      <td>{u.username !== 'admin' && <button onClick={() => deleteUser(u.username)}>Delete</button>}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <h3>Add User</h3>
              <div className="form-row">
                <input placeholder="Username" value={newUser.username} onChange={(e) => setNewUser({ ...newUser, username: e.target.value })} />
                <input type="password" placeholder="Password" value={newUser.password} onChange={(e) => setNewUser({ ...newUser, password: e.target.value })} />
                <label><input type="checkbox" checked={newUser.admin} onChange={(e) => setNewUser({ ...newUser, admin: e.target.checked })} /> Admin</label>
                <button onClick={createUser}>Create</button>
              </div>
              {userError && <p className="error">{userError}</p>}
            </div>
          </div>
        )}
      </main>
      <footer>
        <small>&copy; {new Date().getFullYear()} Minder Alarm System</small>
      </footer>
    </div>
  );
}