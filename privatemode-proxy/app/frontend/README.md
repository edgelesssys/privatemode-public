# Privatemode UI

This is the source code of the Privatemode UI. It's a modified version of [ChatGPT-web](https://github.com/Niek/chatgpt-web).

## Development

The frontend code requires some Wails generated function definitions.
To successfully run the code, and to silence linter errors, run the following to create stubs for the required functions.

```bash
mkdir -p wailsjs/go/main

echo "export function GetConfiguredAPIKey():Promise<string> { return Promise.resolve('') }
" > wailsjs/go/main/ConfigurationService.ts

echo "
export function OnSmokeTestCompleted(arg1:boolean, arg2:string):Promise<void> { return Promise.resolve() }
export function SmokeTestingActivated():Promise<boolean> { return Promise.resolve(false) }
" > wailsjs/go/main/SmokeTestService.ts
```

## Testing

### Frontend-only

If you want to iterate on changes with hot reloading, you can run a dev server for the frontend:

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
