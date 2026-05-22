import { useState } from "react";

export function SettingsPage() {
  // В первой итерации настройки statless и хранятся локально.
  // В v0.2 — REST endpoint в control-plane (PATCH /api/v1/settings).
  const [stun, setStun] = useState(localStorage.getItem("vks4.stun") ?? "stun:coturn:3478");
  const [turnURI, setTurnURI] = useState(localStorage.getItem("vks4.turn.uri") ?? "");
  const [turnUser, setTurnUser] = useState(localStorage.getItem("vks4.turn.user") ?? "");
  const [retention, setRetention] = useState(localStorage.getItem("vks4.retention") ?? "30");
  const [saved, setSaved] = useState(false);

  function save() {
    localStorage.setItem("vks4.stun", stun);
    localStorage.setItem("vks4.turn.uri", turnURI);
    localStorage.setItem("vks4.turn.user", turnUser);
    localStorage.setItem("vks4.retention", retention);
    setSaved(true);
    setTimeout(() => setSaved(false), 2000);
  }

  return (
    <div className="space-y-4 max-w-xl">
      <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>

      <section className="card p-4 space-y-3">
        <h2 className="font-medium">ICE</h2>
        <label className="block">
          <span className="text-sm">STUN URI</span>
          <input className="input mt-1" value={stun} onChange={(e) => setStun(e.target.value)} />
        </label>
        <label className="block">
          <span className="text-sm">TURN URI</span>
          <input className="input mt-1" value={turnURI} onChange={(e) => setTurnURI(e.target.value)} />
        </label>
        <label className="block">
          <span className="text-sm">TURN username</span>
          <input className="input mt-1" value={turnUser} onChange={(e) => setTurnUser(e.target.value)} />
        </label>
      </section>

      <section className="card p-4 space-y-3">
        <h2 className="font-medium">Retention</h2>
        <label className="block">
          <span className="text-sm">Recordings retention (days)</span>
          <input className="input mt-1" type="number" min={1} value={retention} onChange={(e) => setRetention(e.target.value)} />
        </label>
      </section>

      <div className="flex items-center gap-3">
        <button className="btn-primary" onClick={save}>Save</button>
        {saved && <span className="text-sm text-green-600">Saved</span>}
      </div>
    </div>
  );
}
