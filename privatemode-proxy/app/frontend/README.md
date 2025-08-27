# Privatemode UI

This is the source code of the Privatemode UI. It's a modified version of [ChatGPT-web](https://github.com/Niek/chatgpt-web).

## Testing

### Frontend-only

If you want to iterate on changes with hot reloading, you can run a dev server for the frontend:

```bash
mkdir -p wailsjs/go/main

echo "export function GetConfiguredAPIKey () { return '' }
" > wailsjs/go/main/ConfigurationService.js

echo "
export function OnSmokeTestCompleted(arg1, arg2) {}
export function SmokeTestingActivated() { return false }
" > wailsjs/go/main/SmokeTestService.js
```

```bash
nix develop
npm i
npm run dev
```

### With an E2E setup

If you want to test against an actual Privatemode deployment, just run:

```bash
docker compose up --build
```

This will run the app against the current production deployment. You can also run it against a custom deployment by altering the arguments for the `privatemode-proxy` container in the [Docker compose file](./docker-compose.yml) accordingly.
You'll need to enter a valid API key in the Docker compose file in order to run it.
