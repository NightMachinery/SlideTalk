import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const appCss = readFileSync('src/app.css', 'utf8');

describe('mobile audio card layout styles', () => {
  it('moves audio track actions below the title on mobile', () => {
    expect(appCss).toMatch(/@media \(max-width: 840px\)[\s\S]*\.audio-track\s*\{[\s\S]*grid-template-columns:\s*1fr;/);
    expect(appCss).toMatch(/@media \(max-width: 840px\)[\s\S]*\.audio-track\s*>\s*\.settings-actions\s*\{[\s\S]*grid-column:\s*1\s*\/\s*-1;/);
    expect(appCss).toMatch(/@media \(max-width: 840px\)[\s\S]*\.audio-track-main strong\s*\{[\s\S]*-webkit-line-clamp:\s*2;/);
  });
});


describe('audio mini-player scroll layout styles', () => {
  it('keeps audio playback controls visible while track lists scroll', () => {
    expect(appCss).toMatch(/\.audio-mini-player\s*\{[\s\S]*position:\s*sticky;/);
    expect(appCss).toMatch(/\.audio-stage-track-list\s*\{[\s\S]*overflow-y:\s*auto;/);
    expect(appCss).toMatch(/\.audio-panel-track-list\s*\{[\s\S]*overflow-y:\s*auto;/);
  });

  it('uses a compact finish mode menu in the mini-player', () => {
    expect(appCss).toMatch(/\.finish-mode-control\s*\{[\s\S]*position:\s*relative;/);
    expect(appCss).toMatch(/\.finish-mode-trigger\s*\{[\s\S]*width:\s*38px;/);
  });
});
