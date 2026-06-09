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


  it('keeps search controls in the list toolbar instead of the mini-player', () => {
    expect(appCss).toMatch(/\.audio-list-toolbar\s*\{[\s\S]*grid-template-columns:\s*minmax\(180px,\s*1fr\)\s*auto;/);
    expect(appCss).toMatch(/\.audio-search-field\s*\{[\s\S]*position:\s*relative;/);
    expect(appCss).toMatch(/\.audio-search-empty\s*\{[\s\S]*text-align:\s*center;/);
  });



  it('pins the mini-player to the bottom and keeps volume above other popovers', () => {
    expect(appCss).toMatch(/\.audio-mini-player\s*\{[^}]*position:\s*sticky;[^}]*bottom:\s*0;/);
    expect(appCss).not.toMatch(/\.audio-mini-player\s*\{[^}]*top:\s*0;/);
    expect(appCss).toMatch(/\.local-audio-control\s*\{[\s\S]*z-index:\s*180;/);
    expect(appCss).toMatch(/\.local-volume-popover\s*\{[\s\S]*z-index:\s*220;/);
    expect(appCss).toMatch(/\.finish-mode-popover\s*\{[\s\S]*z-index:\s*130;/);
  });

  it('uses a compact responsive audio upload row', () => {
    expect(appCss).toMatch(/\.audio-upload\s*\{[^}]*grid-template-columns:\s*minmax\(0,\s*1fr\)\s*auto;/);
    expect(appCss).toMatch(/@media \(max-width: 840px\)[\s\S]*\.audio-upload\s*\{[\s\S]*grid-template-columns:\s*1fr;/);
  });

});
