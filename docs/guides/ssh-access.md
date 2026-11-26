# Set up SSH access

ship logs in to your server with your own SSH key. It never asks for a password, so the key has to work before you run `ship prod setup`.

## 1. Create a key

On the machine where you run ship:

```sh
ssh-keygen -t ed25519 -f ~/.ssh/prod -C "ship"
```

This writes a private key, `~/.ssh/prod`, which stays on this machine, and a public key, `~/.ssh/prod.pub`, which goes on the server. On Windows, run the same command in PowerShell. Use `$env:USERPROFILE\.ssh\prod` as the path.

## 2. Put the public key on a new server

Choose one:

- **When you create the server:** paste the contents of `~/.ssh/prod.pub` into your provider's SSH key field. Most VPS providers have one.
- **On a server with password login:**

  ```sh
  ssh-copy-id -i ~/.ssh/prod.pub root@<ip>
  ```

  On Windows, where `ssh-copy-id` doesn't exist:

  ```powershell
  type $env:USERPROFILE\.ssh\prod.pub | ssh root@<ip> "mkdir -p ~/.ssh && cat >> ~/.ssh/authorized_keys"
  ```

## 3. Log in once by hand

```sh
ssh -i ~/.ssh/prod root@<ip>
```

Answer `yes` to trust the server's host key. ship can't answer that prompt itself, so it fails on a server you have never connected to.

## 4. Tell ship which key to use

Set `identity_file` on the server in `ship.yaml`:

```yaml
servers:
  prod:
    host: root@<ip>
    identity_file: ~/.ssh/prod
```

Or leave it out, and ship uses your ssh config and agent.

If the key has a passphrase, load it into your agent before running ship. ship can't type the passphrase for you:

```sh
eval "$(ssh-agent -s)"
ssh-add ~/.ssh/prod
```

Now run [setup](add-a-server.md#3-prepare-the-server). It copies root's keys to the admin user (`ops`), so the same key logs in as `ops` afterwards.

## Add another machine or person

After `lock-ssh`, root can't log in. Add new keys to the admin user instead.

1. On the new machine, create a key (step 1) and copy the single line in its `.pub` file.
2. From a machine that already has access, add that line to the admin user's keys:

   ```sh
   echo 'ssh-ed25519 AAAA… ship' | ssh ops@<ip> 'cat >> ~/.ssh/authorized_keys'
   ```

3. On the new machine, check the login and the passwordless `sudo` ship needs:

   ```sh
   ssh -i ~/.ssh/prod -o IdentitiesOnly=yes ops@<ip> 'sudo -n true && echo ok'
   ```

Then set `host: ops@<ip>` and `identity_file` in that machine's `ship.yaml`, and [import the apps](use-from-another-machine.md).

## Remove access

Delete the key's line from `/home/ops/.ssh/authorized_keys` on the server:

```sh
ssh ops@<ip> 'nano ~/.ssh/authorized_keys'
```

Keep at least one key that you know works. `lock-ssh` has already turned off root and password logins, so if you remove every key you can only get back in through your provider's console.

## CI keys

CI deploys use the separate `deploy` user with a restricted key. See [Deploy from CI](deploy-from-ci.md).
