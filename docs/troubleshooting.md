# Troubleshooting

## Startup

- **"config load failed":** the error names the missing or invalid key. Check that `config/config.toml` exists relative to where you launched the manager.
- **Server never reaches `running`:** check `mcsm logs` for the cause (EULA not accepted, bad Java path, not enough memory). If loading is just slow, raise `startup_timeout`.
- **Server keeps restarting:** it is crashing on startup. Read `mcsm logs`. After `max_restarts` attempts the manager stops retrying.

## Ports

If the server won't start because the port is in use, another process (often a leftover `java` from an earlier run) holds port 25565. Find and stop it:

- Windows: `Get-NetTCPConnection -LocalPort 25565 -State Listen`
- Linux: `ss -ltnp | grep 25565`

## Windows

- **Java keeps running after a kill:** set `minecraft.command` to the full path of `java.exe`, not just `java`.

## Network and auth

- **Dashboard unreachable from another machine:** `api.host` is probably still `127.0.0.1`.
- **`api.host = "tailscale"` fails at boot:** Tailscale may not be connected yet. Check `tailscale status`, and start the manager after Tailscale.
- **Manager rejects the config about a missing token:** a token is required when `api.host` is not a loopback address. Set `api.token`, or use `127.0.0.1`.
- **401 from the CLI or API:** the manager has a token set; export `MCSM_TOKEN`.

## Known limitations

- The API is plain HTTP, so the token is visible to anyone sniffing the network.
- A slow stream client can miss messages. Re-fetch `/api/server/status` after reconnecting.
- Readiness detection expects vanilla's "Done" line.
