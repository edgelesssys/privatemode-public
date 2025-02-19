# GPL packages

The Continuum desktop app imports GPL-3.0 licensed code and therefore the main code and it's dependencies must also be licensed with GPL-3.0 or a GPL-3.0 compatible license.
The `internal/gpl` directory groups all packages that are required by the app.
It's important that GPL packages must not depend on any Continuum packages outside `internal/gpl` or external code that is not GPL compatible.
