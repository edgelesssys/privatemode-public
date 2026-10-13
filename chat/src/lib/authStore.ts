import type { Clerk as ClerkType } from '@clerk/clerk-js';
import { ui as clerkUI } from '@clerk/ui';
import type { AuthCredential } from 'privatemode-ai';
import { derived, get, writable } from 'svelte/store';

export interface OrgMembership {
  id: string;
  name: string;
  imageUrl: string | null;
}

export const clerkInstance = writable<ClerkType | null>(null);
export const clerkLoaded = writable(false);
export const isSignedIn = writable(false);
export const userDisplayName = writable<string | null>(null);
export const userImageUrl = writable<string | null>(null);
export const orgName = writable<string | null>(null);
export const orgId = writable<string | null>(null);
export const userOrganizations = writable<OrgMembership[]>([]);

// Limited mode is active for anonymous (signed-out) users. They authenticate
// against the API gateway anonymously.
export const isLimitedMode = derived(isSignedIn, ($isSignedIn) => !$isSignedIn);

// authIdentity changes whenever the effective authentication identity changes
// (sign-in/out or active organization). The Privatemode client should be
// re-initialized when it changes, as the resolved internal key (and therefore
// the encryption secret) depends on it.
export const authIdentity = derived(
  [isSignedIn, orgId],
  ([$isSignedIn, $orgId]) =>
    $isSignedIn ? `jwt:${$orgId ?? ''}` : 'anonymous',
);
// getAnonymousFingerprint should eventually collect some client-side telemetry
// to augment abuse control. For now, it's just a static value
function getAnonymousFingerprint(): string {
  return 'placeholder';
}

// getAuthCredential resolves the credential to authenticate the next Privatemode
// request with. Signed-in users authenticate with their Clerk session JWT;
// signed-out users authenticate anonymously. It is passed to the Privatemode SDK
// as the `auth` provider and invoked before every request, so JWTs are always
// fresh (Clerk's getToken() refreshes them transparently).
export async function getAuthCredential(): Promise<AuthCredential> {
  const clerk = get(clerkInstance);
  const session = clerk?.session;
  if (session) {
    const token = await session.getToken();
    if (token) {
      return { type: 'jwt', value: token };
    }
    throw new Error('Clerk session present but no token could be resolved');
  }
  // @ts-expect-error Anonymous auth is supported internally but intentionally excluded from the public SDK types.
  return { type: 'anonymous', value: getAnonymousFingerprint() };
}

// Attribution parameters the marketing site appends to links leaving for a
// product surface (privatemode.ai, decorateOutboundUrl in lib/analytics.ts).
// pm_entry is the page on the website the visitor entered through, pm_ref the
// referring domain.
const ATTRIBUTION_PARAMS = [
  'utm_source',
  'utm_medium',
  'utm_campaign',
  'utm_term',
  'utm_content',
  'gclid',
  'gbraid',
  'wbraid',
  'gad_source',
  'gad_campaignid',
  'msclkid',
  'fbclid',
  'li_fat_id',
  'pm_entry',
  'pm_ref',
];

let landingAttribution: Record<string, string> | undefined;

/**
 * Attribution parameters of the URL this visit landed on, read once and kept in
 * a module variable.
 *
 * Cached rather than read on demand because signing up happens in a Clerk modal
 * that leaves the address bar alone, but a route change in between would not,
 * and by the time the portal redirect fires the query string may be gone.
 * Nothing is written to the device, so this needs no consent.
 */
function getLandingAttribution(): Record<string, string> {
  if (landingAttribution) return landingAttribution;

  const found: Record<string, string> = {};
  if (typeof window !== 'undefined') {
    const params = new URLSearchParams(window.location.search);
    for (const key of ATTRIBUTION_PARAMS) {
      const value = params.get(key);
      if (value) found[key] = value;
    }
  }

  landingAttribution = found;
  return found;
}

/**
 * Carries the attribution parameters of this visit over to the portal.
 *
 * Without this the chain breaks here. Someone arrives from a campaign or a
 * podcast link, tries the chat anonymously, then creates an account — and the
 * portal has no idea what sent them, because a plain redirect drops the query
 * string. Visitors who reach the portal through the chat rather than directly
 * would otherwise lose their attribution at this hop.
 *
 * Parameters already on the target URL win.
 */
function withAttribution(target: string): string {
  if (typeof window === 'undefined') return target;

  const url = new URL(target);
  for (const [key, value] of Object.entries(getLandingAttribution())) {
    if (!url.searchParams.has(key)) {
      url.searchParams.set(key, value);
    }
  }
  return url.toString();
}

export async function initializeAuth(): Promise<void> {
  // Read the landing URL before Clerk is fetched and anything can navigate.
  getLandingAttribution();

  const publishableKey = import.meta.env.VITE_CLERK_PUBLISHABLE_KEY;
  if (!publishableKey) {
    console.warn('No Clerk publishable key configured; auth disabled');
    clerkLoaded.set(true);
    return;
  }

  try {
    const { Clerk } = await import('@clerk/clerk-js');
    const clerk = new Clerk(publishableKey);
    await clerk.load({
      ui: clerkUI,
      // openSignIn() renders a modal that routes virtually, so Clerk does not
      // send anyone to these URLs today. They are decorated anyway, so a switch
      // to a redirect flow does not silently break attribution again.
      signInUrl: withAttribution('https://portal.privatemode.ai/sign-in'),
      signUpUrl: withAttribution('https://portal.privatemode.ai/sign-up'),
    });

    clerkInstance.set(clerk);
    clerkLoaded.set(true);

    const updateAuthState = () => {
      const session = clerk.session;
      const user = clerk.user;
      const org = clerk.organization;

      if (session && user) {
        isSignedIn.set(true);
        userDisplayName.set(
          user.fullName || user.primaryEmailAddress?.emailAddress || 'User',
        );
        userImageUrl.set(user.imageUrl || null);
        orgName.set(org?.name || null);
        orgId.set(org?.id || null);

        user
          .getOrganizationMemberships()
          .then(({ data }) => {
            userOrganizations.set(
              data.map((m) => ({
                id: m.organization.id,
                name: m.organization.name,
                imageUrl: m.organization.imageUrl || null,
              })),
            );
            if (data.length === 0) {
              // The hop that matters for attribution. Signing up happens in a
              // Clerk modal on this origin, so a new account reaches the portal
              // for the first time right here, with no organization yet.
              window.location.href = withAttribution(
                'https://portal.privatemode.ai/create-organization',
              );
            }
          })
          .catch((err: unknown) => {
            console.error('Failed to fetch organization memberships:', err);
            userOrganizations.set([]);
          });
      } else {
        isSignedIn.set(false);
        userDisplayName.set(null);
        userImageUrl.set(null);
        orgName.set(null);
        orgId.set(null);
        userOrganizations.set([]);
      }
    };

    clerk.addListener(updateAuthState);
  } catch (err) {
    console.error('Failed to initialize Clerk:', err);
    clerkLoaded.set(true);
  }
}

export async function switchOrganization(
  organizationId: string,
): Promise<void> {
  let clerk: ClerkType | null = null;
  clerkInstance.subscribe((c) => (clerk = c))();
  if (clerk) {
    await (clerk as ClerkType).setActive({ organization: organizationId });
    // Clear the privatemode session cache so a new session is established for
    // the newly active organization (which resolves to a different internal key).
    sessionStorage.removeItem('privatemode-secret');
    sessionStorage.removeItem('privatemode-manifest');
  }
}

export async function signIn(): Promise<void> {
  let clerk: ClerkType | null = null;
  clerkInstance.subscribe((c) => (clerk = c))();
  if (clerk) {
    await (clerk as ClerkType).openSignIn();
  }
}

export async function signOut(): Promise<void> {
  let clerk: ClerkType | null = null;
  clerkInstance.subscribe((c) => (clerk = c))();
  if (clerk) {
    // Clear the privatemode session cache so a new (anonymous) session is created.
    sessionStorage.removeItem('privatemode-secret');
    sessionStorage.removeItem('privatemode-manifest');
    await (clerk as ClerkType).signOut();
  }
}
