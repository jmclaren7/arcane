<!--
	Installed from @ieedan/shadcn-svelte-extras
-->

<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { UseClipboard } from '#lib/hooks/use-clipboard.svelte';
	import { cn } from '#lib/utils';
	import { scale } from 'svelte/transition';
	import type { CopyButtonProps } from './types';
	import { CopyIcon, CloseIcon, CheckIcon } from '#lib/icons';
	import { onMount } from 'svelte';
	import { m } from '#lib/paraglide/messages';

	let {
		ref = $bindable(null),
		text,
		icon,
		animationDuration = 500,
		variant = 'ghost',
		size = 'icon',
		onCopy,
		class: className,
		tabindex = -1,
		children
	}: CopyButtonProps = $props();
	void ref;

	const clipboard = new UseClipboard();
	void scale;

	const resolvedSize = $derived(size === 'icon' && children ? 'default' : size);

	// The Clipboard API is only exposed in secure contexts. When it's unavailable
	// (usually an insecure/non-HTTPS connection) we hide the button entirely.
	let canCopy = $state(true);

	onMount(() => {
		canCopy = typeof navigator !== 'undefined' && !!navigator.clipboard;
	});
</script>

{#snippet idleIcon()}
	<div
		class="col-start-1 row-start-1"
		in:scale={{ duration: animationDuration, start: 0.85 }}
		out:scale={{ duration: animationDuration, start: 0.85 }}
	>
		{#if icon}
			{@render icon()}
		{:else}
			<CopyIcon tabindex={-1} />
		{/if}
		<span class="sr-only">{m.common_copy()}</span>
	</div>
{/snippet}

{#if canCopy}
	<ArcaneButton
		bind:ref
		action="base"
		tone={variant === 'ghost' ? 'ghost' : variant === 'outline' ? 'outline' : 'outline'}
		size={resolvedSize}
		{tabindex}
		class={cn('flex items-center gap-2', className)}
		type="button"
		name="copy"
		onclick={async () => {
			const status = await clipboard.copy(text);

			onCopy?.(status);
		}}
	>
		<span class="grid place-items-center">
			{#if clipboard.status === 'success'}
				<div
					class="col-start-1 row-start-1"
					in:scale={{ duration: animationDuration, start: 0.85 }}
					out:scale={{ duration: animationDuration, start: 0.85 }}
				>
					<CheckIcon tabindex={-1} />
					<span class="sr-only">{m.common_copied()}</span>
				</div>
			{:else if clipboard.status === 'failure'}
				<div
					class="col-start-1 row-start-1"
					in:scale={{ duration: animationDuration, start: 0.85 }}
					out:scale={{ duration: animationDuration, start: 0.85 }}
				>
					<CloseIcon tabindex={-1} />
					<span class="sr-only">{m.common_copy_failed()}</span>
				</div>
			{:else}
				{@render idleIcon()}
			{/if}
		</span>
		{@render children?.()}
	</ArcaneButton>
{/if}
