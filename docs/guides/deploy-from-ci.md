# Deploy from CI

The `deploy` user created by setup may only run `ship-agent deploy`. Add a CI key to `/var/lib/deploy/.ssh/authorized_keys` with a forced command:

```text
command="sudo /usr/local/sbin/ship-agent deploy \"$SSH_ORIGINAL_COMMAND\"",restrict ssh-ed25519 AAAA… ci
```

The restricted key is constrained to `sudo /usr/local/sbin/ship-agent deploy`. This is unchanged by the server-backed configuration model.

CI sends the app and tag as one argument, which the agent splits and validates:

```sh
ssh deploy@HOST 'hello v1.2.3'
```

- CI needs only the host and key. It does not need a local `ship.yaml`, source checkout or app records.
- The desired configuration must already be on the server. Save it first with `ship <server> app create`, `ship <server> app edit`, `ship <server> app set` or `ship <server> config edit`.
- CI cannot upload config, compose files or Dockerfiles, change secrets, or run any other ship action. The key only runs the deploy agent.
- The deploy uses the app's desired record, deploy commands and maintenance setting, and builds release apps on the server. It promotes desired to running on success.
- It can deploy any release of a GitHub release app.
- It can only redeploy an image app version that is already on the server, because the `deploy` user can't load images. Build and load a new image with a local `ship <server> deploy <app>` first.
- Store the host key in CI (`ssh-keyscan HOST`) rather than turning off host key checking.

If you have changed an app's desired config and want CI to apply it, save the change on the server, then run the CI deploy. `ship <server> status` shows `pending deploy` until it runs; `diff` shows what will change.

Setup also writes `/etc/sudoers.d/91-deploy`, which allows only `ship-agent deploy`. See [Add a server](add-a-server.md) and [Deploy an app](deploy-an-app.md).
