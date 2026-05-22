import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { RoomsApi, type Room } from "@/features/rooms/api";

export function RoomCreate() {
  const nav = useNavigate();
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [mode, setMode] = useState<Room["mode"]>("meeting");
  const [maxParticipants, setMax] = useState(50);
  const [waitingRoom, setWaitingRoom] = useState(false);

  const create = useMutation({
    mutationFn: () => RoomsApi.create({ name, description: desc, mode, maxParticipants, waitingRoom }),
    onSuccess: (r) => { qc.invalidateQueries({ queryKey: ["rooms"] }); nav(`/rooms/${r.id}`); },
  });

  function submit(e: FormEvent) {
    e.preventDefault();
    create.mutate();
  }

  return (
    <form onSubmit={submit} className="space-y-4 max-w-xl">
      <h1 className="text-2xl font-semibold tracking-tight">Create room</h1>
      <label className="block">
        <span className="text-sm">Name</span>
        <input className="input mt-1" value={name} onChange={(e) => setName(e.target.value)} required />
      </label>
      <label className="block">
        <span className="text-sm">Description</span>
        <textarea className="input mt-1" rows={3} value={desc} onChange={(e) => setDesc(e.target.value)} />
      </label>
      <div className="grid grid-cols-2 gap-4">
        <label className="block">
          <span className="text-sm">Mode</span>
          <select className="input mt-1" value={mode} onChange={(e) => setMode(e.target.value as Room["mode"])}>
            <option value="meeting">Meeting</option>
            <option value="lecture">Lecture</option>
            <option value="webinar">Webinar</option>
          </select>
        </label>
        <label className="block">
          <span className="text-sm">Max participants</span>
          <input className="input mt-1" type="number" min={1} value={maxParticipants} onChange={(e) => setMax(parseInt(e.target.value || "1", 10))} />
        </label>
      </div>
      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" checked={waitingRoom} onChange={(e) => setWaitingRoom(e.target.checked)} />
        Enable waiting room
      </label>
      {create.error && <div className="text-red-600 text-sm">{(create.error as Error).message}</div>}
      <div className="flex gap-2">
        <button className="btn-primary" disabled={create.isPending}>Create</button>
        <button type="button" className="btn-ghost" onClick={() => history.back()}>Cancel</button>
      </div>
    </form>
  );
}
