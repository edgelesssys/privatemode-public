<script lang="ts">
  import {
    ArrowLeft,
    ShieldCheck,
    CircleCheck,
    BadgeCheck,
    ExternalLink,
    Hammer,
    Cpu,
  } from 'lucide-svelte';
  import { goto } from '$app/navigation';
  import { privatemodeClient } from '$lib/clientStore';

  let manifestHash = $state('');
  let trustedMeasurement = $state('');
  let productLine = $state('');
  let minimumTCB = $state<{
    BootloaderVersion: number;
    TEEVersion: number;
    SNPVersion: number;
    MicrocodeVersion: number;
  } | null>(null);

  $effect(() => {
    const client = $privatemodeClient;
    if (!client?.manifest) {
      return;
    }

    const manifest = JSON.stringify(client.manifest);

    crypto.subtle
      .digest('SHA-256', new TextEncoder().encode(manifest))
      .then((hashBuffer) => {
        const hashArray = Array.from(new Uint8Array(hashBuffer));
        manifestHash = hashArray
          .map((b) => b.toString(16).padStart(2, '0'))
          .join('');
      });

    try {
      const parsed = JSON.parse(manifest);
      trustedMeasurement =
        parsed?.ReferenceValues?.snp?.[0]?.TrustedMeasurement ?? '';
      minimumTCB = parsed?.ReferenceValues?.snp?.[0]?.MinimumTCB ?? null;
      productLine = parsed?.ReferenceValues?.snp?.[0]?.ProductName ?? '';
    } catch {
      // Invalid JSON, leave defaults
    }
  });

  function handleBack() {
    goto('/');
  }
</script>

<div class="security-container">
  <div class="security-header">
    <button
      onclick={handleBack}
      class="back-btn"
    >
      <ArrowLeft size={20} />
      Back
    </button>
    <h1>Security</h1>
  </div>

  <div class="security-content">
    <section class="security-section highlight-section">
      <div class="section-header">
        <ShieldCheck
          size={28}
          color="var(--color-accent-green)"
        />
        <h2>Your session is secure</h2>
      </div>
      <p class="section-description">
        Your connection to Privatemode is protected by confidential computing
        technology.
      </p>
    </section>

    <section class="security-section verified-section">
      <div class="check-badge">
        <CircleCheck
          size={20}
          color="#22c55e"
        />
      </div>
      <div class="section-header">
        <BadgeCheck size={24} />
        <h2>Remote attestation</h2>
      </div>
      <p class="section-description">
        Before establishing a connection, the security of the Privatemode
        deployment is cryptographically verified. This proves that all
        components within the deployment run nothing but the expected code.
      </p>
      {#if manifestHash}
        <div class="data-block">
          <span class="data-label">Manifest hash (SHA-256)</span>
          <code class="data-value">{manifestHash}</code>
        </div>
        <a
          href="https://docs.privatemode.ai/guides/verify-source"
          target="_blank"
          rel="noopener noreferrer"
          class="section-link"
        >
          <ExternalLink size={16} />
          Learn how to reproduce this hash
        </a>
      {:else}
        <p class="data-loading">Loading...</p>
      {/if}
    </section>

    <section class="security-section verified-section">
      <div class="check-badge">
        <CircleCheck
          size={20}
          color="#22c55e"
        />
      </div>
      <div class="section-header">
        <Hammer size={24} />
        <h2>Reproducible software</h2>
      </div>
      <p class="section-description">
        The initial memory contents of each virtual machine running the
        workloads is cryptographically verified before connecting, proving that
        the machines have not been tampered with.
      </p>
      {#if trustedMeasurement}
        <div class="data-block">
          <span class="data-label">Trusted measurement</span>
          <code class="data-value">{trustedMeasurement}</code>
        </div>
        <a
          href="https://docs.privatemode.ai/guides/verify-source"
          target="_blank"
          rel="noopener noreferrer"
          class="section-link"
        >
          <ExternalLink size={16} />
          Learn how to reproduce this hash
        </a>
      {:else}
        <p class="data-loading">Loading...</p>
      {/if}
    </section>

    <section class="security-section verified-section">
      <div class="check-badge">
        <CircleCheck
          size={20}
          color="#22c55e"
        />
      </div>
      <div class="section-header">
        <Cpu size={24} />
        <h2>Hardware-based security</h2>
      </div>
      <p class="section-description">
        When connecting to Privatemode, the app verifies that all the hardware
        components are up-to-date and that the latest security updates are
        available. Below, you see the minimal version numbers of each of the
        chips' system components accepted by Privatemode.
      </p>
      {#if productLine}
        <div class="data-block">
          <span class="data-label">Product line</span>
          <code class="data-value">{productLine}</code>
        </div>
      {/if}
      {#if minimumTCB}
        <div class="tcb-grid">
          <div class="tcb-item">
            <span class="tcb-label">Bootloader</span>
            <span class="tcb-value">{minimumTCB.BootloaderVersion}</span>
          </div>
          <div class="tcb-item">
            <span class="tcb-label">TEE</span>
            <span class="tcb-value">{minimumTCB.TEEVersion}</span>
          </div>
          <div class="tcb-item">
            <span class="tcb-label">SNP</span>
            <span class="tcb-value">{minimumTCB.SNPVersion}</span>
          </div>
          <div class="tcb-item">
            <span class="tcb-label">Microcode</span>
            <span class="tcb-value">{minimumTCB.MicrocodeVersion}</span>
          </div>
        </div>
      {:else}
        <p class="data-loading">Loading...</p>
      {/if}
    </section>

    <section class="security-section learn-more-section">
      <a
        href="https://docs.privatemode.ai/"
        target="_blank"
        rel="noopener noreferrer"
        class="learn-more-link"
      >
        <ExternalLink size={20} />
        Learn more about how Privatemode protects your data
      </a>
    </section>
  </div>
</div>

<style>
  .security-container {
    margin: 0 auto;
    max-width: 840px;
    padding: 0 20px;
    height: 100%;
    display: flex;
    flex-direction: column;
    box-sizing: border-box;
  }

  .security-header {
    padding-top: 20px;
    padding-bottom: 20px;
    position: relative;
    flex-shrink: 0;
    background: var(--color-bg-page);
  }

  .security-header::after {
    content: '';
    position: absolute;
    bottom: -20px;
    left: calc(-50vw + 50%);
    right: calc(-50vw + 50%);
    height: 20px;
    background: linear-gradient(to bottom, var(--color-bg-page), transparent);
    pointer-events: none;
    z-index: 1;
  }

  .security-content {
    flex: 1;
    overflow-y: auto;
    padding-top: 20px;
    padding-bottom: 80px;
    --scrollbar-color: transparent;
    display: flex;
    flex-direction: column;
    gap: 20px;
  }

  .security-content:hover {
    --scrollbar-color: var(--color-scrollbar);
  }

  .security-content::-webkit-scrollbar {
    width: 4px;
  }

  .security-content::-webkit-scrollbar-thumb {
    border-radius: 2px;
    background: var(--scrollbar-color);
    transition: background 0.2s;
  }

  .back-btn {
    display: flex;
    align-items: center;
    gap: 8px;
    background: none;
    border: none;
    color: var(--color-text-muted);
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    padding: 8px 0;
    margin-bottom: 16px;
    font-family: 'Inter Variable', sans-serif;
    transition: color 0.2s;
  }

  .back-btn:hover {
    color: var(--color-text-heading);
  }

  .security-header h1 {
    font-size: 32px;
    font-weight: 600;
    color: var(--color-text-heading);
    margin: 0;
  }

  .security-section {
    background: var(--color-bg-surface);
    border-radius: 12px;
    padding: 24px;
    position: relative;
  }

  .verified-section {
    padding-right: 48px;
  }

  .check-badge {
    position: absolute;
    top: 16px;
    right: 16px;
    width: 28px;
    height: 28px;
    background: var(--color-success-bg);
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .highlight-section {
    border: 1px solid var(--color-accent-green);
    background: linear-gradient(
      135deg,
      var(--color-success-bg) 0%,
      var(--color-bg-surface) 100%
    );
  }

  .section-header {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 12px;
    color: var(--color-text-heading);
  }

  .security-section h2 {
    font-size: 18px;
    font-weight: 600;
    color: var(--color-text-heading);
    margin: 0;
  }

  .section-description {
    color: var(--color-text-muted);
    font-size: 14px;
    line-height: 1.6;
    margin: 0;
  }

  .data-block {
    margin-top: 16px;
    padding: 12px 16px;
    background: var(--color-bg-surface-tertiary);
    border-radius: 8px;
  }

  .data-label {
    display: block;
    font-size: 12px;
    font-weight: 500;
    color: var(--color-text-tertiary);
    margin-bottom: 6px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .data-value {
    display: block;
    font-size: 13px;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    color: var(--color-text-heading);
    word-break: break-all;
    line-height: 1.5;
  }

  .data-loading {
    margin-top: 12px;
    color: var(--color-text-tertiary);
    font-size: 13px;
    font-style: italic;
  }

  .section-link {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin-top: 12px;
    color: var(--color-accent);
    text-decoration: none;
    font-size: 13px;
    font-weight: 500;
  }

  .section-link:hover {
    text-decoration: underline;
  }

  .tcb-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 12px;
    margin-top: 16px;
  }

  .tcb-item {
    padding: 12px;
    background: var(--color-bg-surface-tertiary);
    border-radius: 8px;
    text-align: center;
  }

  .tcb-label {
    display: block;
    font-size: 11px;
    font-weight: 500;
    color: var(--color-text-tertiary);
    margin-bottom: 4px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .tcb-value {
    font-size: 18px;
    font-weight: 600;
    color: var(--color-text-heading);
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
  }

  .learn-more-link {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--color-accent);
    text-decoration: none;
    font-size: 14px;
    font-weight: 500;
  }

  .learn-more-link:hover {
    text-decoration: underline;
  }

  @media (max-width: 768px) {
    .back-btn {
      display: none;
    }
    .tcb-grid {
      grid-template-columns: repeat(2, 1fr);
    }
    .security-header h1 {
      font-size: 24px;
    }
  }
</style>
