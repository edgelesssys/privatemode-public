<script lang="ts">
  import DOMPurify from 'dompurify'
  import Typeahead from 'svelte-typeahead'
  import prompts from '../awesome-chatgpt-prompts/prompts.csv'

  const inputPrompt = (prompt: string) => {
    input.value = prompt
    input.style.height = 'auto'
    input.style.height = input.scrollHeight + 'px'
  }

  const extract = (prompt: typeof prompts[0]) => prompt.act

  export let input : HTMLTextAreaElement
</script>

{#if input}
<div class="columns is-left mt-1 ml-5">
  <div class="column">
    <p class="select-title">You can select a pre-made prompt</p>
  </div>
</div>
<div class="columns is-left ml-5">
  <div class="column is-half">
    <Typeahead
      data={prompts}
      {extract}
      label="Select a pre-made prompt"
      hideLabel
      showDropdownOnFocus
      showAllResultsOnFocus
      inputAfterSelect="clear"
      on:select={({ detail }) => inputPrompt(detail.original.prompt)}
      placeholder="See all pre-made prompts"
      let:result
    >
      <a class="dropdown-item" href="#top" on:click|preventDefault title="{result.original.prompt}">
        <!--
          Sanitize result.string because Typeahead introduces HTML tags and prompt
          strings are untrusted.
        -->
        {@html DOMPurify.sanitize(result.string, { ALLOWED_TAGS: ['mark'] })}
      </a>
    </Typeahead>
  </div>
</div>

<div class="columns is-left ml-4">
  <div class="column is-half fw-medium mt-5">Or start typing below!</div>
</div>
{/if}
