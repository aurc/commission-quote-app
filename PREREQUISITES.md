# Prerequisites

Everything you need to run the Commission Quote App, and how to install it.

**macOS only.** Every command here was run on the machine this project was built on. Nothing is
written for Windows or Linux, because none of it was tested there and untested instructions are worse
than none: they fail in ways that look like the project is broken. The tools themselves are all cross
platform, so if you are on another operating system, install the versions in the table below from the
vendors' own pages and the rest of the project works the same.

## Which path do you need?

| You want to | You need |
|---|---|
| Run the app and click around | **Docker only** |
| Read the code, run the tests, change something | **Go and Node**, Docker optional |

The Docker path is one command and needs nothing else installed. Take it unless you intend to work on
the code.

| Tool | Version | Needed for | Official install guide |
|---|---|---|---|
| Docker Desktop | Compose v2 or newer | Running the whole stack | [docs.docker.com](https://docs.docker.com/desktop/setup/install/mac-install/) |
| Go | 1.26 or newer | Building and testing the services | [go.dev/doc/install](https://go.dev/doc/install) |
| Node.js | 22 or newer | Building and testing the front end | [nodejs.org/download](https://nodejs.org/en/download) |
| Homebrew | any | Installing the above | [brew.sh](https://brew.sh) |
| Make, Git | any | Makefile shortcuts, cloning | Ship with the Xcode command line tools |

The steps below are the short version for macOS. Where they and a vendor's page disagree, **the
vendor's page is right**: they change their installers and this file does not.

---

## 1. Install Homebrew

Homebrew installs everything else. Skip this if `brew --version` already prints a version.

```sh
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

The installer prints two commands at the end to add Homebrew to your `PATH`. **Run them**, or nothing
below will be found. On Apple Silicon they look like this:

```sh
echo 'eval "$(/opt/homebrew/bin/brew shellenv)"' >> ~/.zprofile
eval "$(/opt/homebrew/bin/brew shellenv)"
```

## 2. Install the tools

```sh
brew install go node git
```

Homebrew currently ships Go 1.27 and Node 26, both comfortably newer than this project needs.

`make` comes with the Xcode command line tools. If `make --version` fails:

```sh
xcode-select --install
```

## 3. Install Docker Desktop

Docker is what runs the whole stack with one command. Two ways to install it; either is fine.

**With Homebrew:**

```sh
brew install --cask docker-desktop
```

**Or download it**, if you would rather not use Homebrew for a GUI application. Docker's own guide is
at https://docs.docker.com/desktop/setup/install/mac-install/ and is the authority if this differs:

1. Go to https://www.docker.com/products/docker-desktop/
2. Download the build for your chip. **Apple Silicon** for M1 through M4, **Intel** for older Macs.
   If you are unsure: Apple menu → About This Mac. A "Chip" line means Apple Silicon, a "Processor"
   line means Intel.
3. Open the downloaded `.dmg` and drag **Docker** into **Applications**.

### Start it, once, by hand

Docker Desktop has to run before any `docker` command works, and it does not start itself after
installation.

1. Open **Docker** from Applications.
2. macOS will ask you to confirm an application downloaded from the internet. Accept.
3. Docker asks for the recommended settings and may ask for your password, to install its command
   line tools. Accept.
4. Accept the service agreement.
5. Wait for the whale icon in the menu bar to **stop animating**. Until it settles, Docker is still
   starting and every command fails with an error that does not explain why.

You can skip the sign in prompt. An account is not needed for anything in this project.

### Check it works

```sh
docker run --rm hello-world
```

That downloads a tiny image and prints a paragraph beginning "Hello from Docker!". If you see it,
Docker is working. If you see `Cannot connect to the Docker daemon`, Docker Desktop is not running
yet: open it and wait for the whale to settle.

Leave Docker Desktop running while you use the project. Quitting it stops the containers.

## 4. Check everything

```sh
go version              # go1.26 or newer
node --version          # v22 or newer
docker compose version  # v2 or newer
make --version
```

All four should print a version. If one does not, open a new terminal first: an installer that
changed your `PATH` does not affect a shell that was already open.

---

## Common problems

| Symptom | Cause and fix |
|---|---|
| `Cannot connect to the Docker daemon` | Docker Desktop is not running, or is still starting. Open it and wait |
| `go: command not found` after installing | Your `PATH` was not updated. Open a new terminal, or run the command the installer printed |
| `unsupported engine` from npm | Node is older than 22. Check `node --version`, then `brew upgrade node` |
| `bind: address already in use` | Something already holds 8080, 8081, 8082, 8083 or 5173. See below |
| `config CQAPI_API_KEY: is required but not set` | You are running a service natively without a `.env`. Run `make env` |

### Freeing a port

```sh
# what is holding 8080
lsof -nP -iTCP:8080 -sTCP:LISTEN

# stop it
kill -9 $(lsof -nP -tiTCP:8080 -sTCP:LISTEN)
```

Kill by **port**, not by process name. The name you expect is often not the process actually
listening, and a stale server answering your requests looks exactly like everything working.

---

Next: [running the app](README.md#getting-started).
