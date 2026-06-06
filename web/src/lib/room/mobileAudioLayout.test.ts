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
