# Deploy from CI

The `deploy` user created by setup may only run `ship-agent deploy`. Add a CI key to `/var/lib/deploy/.ssh/authorized_keys` with a forced command:

```text
command="sudo /usr/local/sbin/ship-agent deploy \"$SSH_ORIGINAL_COMMAND\"",restrict ssh-ed25519 AAAA… ci
```

CI sends the app and tag as one argument, which the agent splits and validates:

```sh
ssh deploy@HOST 'hello v1.2.3'
```

- Store the host key in CI (`ssh-keyscan HOST`) rather than turning off host key checking.
- CI uses the deploy commands and maintenance setting from the last `ship` deploy.
- It can deploy any release of a GitHub release app.
- It can only redeploy an image app version that is already on the server, because the `deploy` user can't load images.

Setup also writes `/etc/sudoers.d/91-deploy`, which allows only `ship-agent deploy`. See [Add a server](add-a-server.md) and [Deploy an app](deploy-an-app.md).
