"""Correspondent icon options, drawn in VectorCraft through its MCP server (vectorcraft-cli mcp --headless).

    python3 build.py              # every option
    python3 build.py wire byline  # just these

Each option is drawn once per variant (light tile, dark tile, and a pixel-fitted 16 px "small" design of
each) into its own document, saved as source/<option>-<variant>.vectorcraft, and exported by VectorCraft
to exports/<option>/: <option>-<theme>.svg, <option>-<theme>-{180,512,1024}.png from the full design and
<option>-<theme>-{16,32}.png from the small design (plus <option>-<theme>-small.svg).

Geometry is written on a 64-unit grid (the old favicon's) and scaled to the artboard, so 1 unit = 16 pt
on the 1024 artboard. The small designs are written directly in pixels on a 16 px artboard.
"""
import math, os, sys
from lib import tool, run

S = os.path.dirname(os.path.abspath(__file__))
B = os.path.dirname(S)
TMP = os.environ.get('VC_TMP', S + '/.tmp')

# ---- palette (ui/src/app.css) ------------------------------------------------------------------
FOG, SHEET, INK, SLATE, RULE = '#EEF1F5', '#FFFFFF', '#18213A', '#5B6478', '#D9DEE7'
PRUSSIAN, GOLD = '#2742C7', '#B7791F'
D_BG, D_SURFACE, D_ACCENT = '#10151F', '#161D2A', '#8EA2FF'


def mix(a, b, t):
    """a over b at opacity t, as a solid colour (no live transparency in the icon)."""
    pa = [int(a[i:i + 2], 16) for i in (1, 3, 5)]
    pb = [int(b[i:i + 2], 16) for i in (1, 3, 5)]
    return '#%02X%02X%02X' % tuple(round(x * t + y * (1 - t)) for x, y in zip(pa, pb))


# ---- geometry helpers --------------------------------------------------------------------------
def f(v):
    return f'{v:.3f}'.rstrip('0').rstrip('.')


def arc_cubics(cx, cy, r, a0, a1):
    """Cubic Béziers for a circular arc from angle a0 to a1 (degrees, y down, clockwise positive)."""
    n = max(1, math.ceil(abs(a1 - a0) / 90 - 1e-9))
    out, step = [], (a1 - a0) / n
    for i in range(n):
        t0, t1 = math.radians(a0 + i * step), math.radians(a0 + (i + 1) * step)
        k = 4 / 3 * math.tan((t1 - t0) / 4)
        p0 = (cx + r * math.cos(t0), cy + r * math.sin(t0))
        p3 = (cx + r * math.cos(t1), cy + r * math.sin(t1))
        c1 = (p0[0] - k * r * math.sin(t0), p0[1] + k * r * math.cos(t0))
        c2 = (p3[0] + k * r * math.sin(t1), p3[1] - k * r * math.cos(t1))
        out.append((p0, c1, c2, p3))
    return out


def squircle(x, y, w, h, R, xi=0.6):
    """Continuous-curvature rounded rectangle (Figma corner smoothing xi; 0.6 matches iOS/macOS icons).
    Each corner is cubic, circular arc, cubic: curvature ramps up from the straight edge instead of
    jumping, which is what makes Apple's tiles read softer than a plain rx."""
    p = (1 + xi) * R
    arc_deg = 90 * (1 - xi)
    arc_len = math.sin(math.radians(arc_deg / 2)) * R * math.sqrt(2)
    alpha = (90 - arc_deg) / 2
    p3p4 = R * math.tan(math.radians(alpha / 2))
    beta = 45 * xi
    c = p3p4 * math.cos(math.radians(beta))
    d = c * math.tan(math.radians(beta))
    b = (p - arc_len - c - d) / 3
    a = 2 * b
    # one corner in a local frame: origin on the incoming edge, p before the corner; u runs along that
    # edge toward the corner, v along the outgoing edge (into the shape's side). Figma's construction.
    def corner(ox, oy, ux, uy, vx, vy):
        P = lambda s, t: (ox + s * ux + t * vx, oy + s * uy + t * vy)
        C = lambda *pts: 'C' + ' '.join(f'{f(P(*q)[0])} {f(P(*q)[1])}' for q in pts) + ' '
        seg = C((a, 0), (a + b, 0), (a + b + c, d))
        ccx, ccy = p - R, R                              # the circular part's centre
        th0 = math.degrees(math.atan2(d - ccy, a + b + c - ccx))
        th1 = math.degrees(math.atan2(d + arc_len - ccy, a + b + c + arc_len - ccx))
        for (_, q1, q2, q3) in arc_cubics(ccx, ccy, R, th0, th1):
            seg += C(q1, q2, q3)
        seg += C((p, d + arc_len + c), (p, d + arc_len + b + c), (p, p))
        return P(0, 0), seg
    # corners: top-right, bottom-right, bottom-left, top-left (clockwise in y-down space)
    tr = corner(x + w - p, y, 1, 0, 0, 1)
    br = corner(x + w, y + h - p, 0, 1, -1, 0)
    bl = corner(x + p, y + h, -1, 0, 0, -1)
    tl = corner(x, y + p, 0, -1, 1, 0)
    d_ = f'M{f(x + p)} {f(y)} '
    for s0, seg in (tr, br, bl, tl):
        d_ += f'L{f(s0[0])} {f(s0[1])} ' + seg
    return d_ + 'Z'


# ---- drawing primitives (all in artboard points) ------------------------------------------------
class Pen:
    """Maps design units to artboard points. The tile uses (k, ox, oy) as is; the glyph is additionally
    scaled by s about the tile centre (s < 1 for the maskable safe zone). tile_mode: 'squircle' (the
    rounded tile), 'square' (full bleed, for apple-touch and maskable), with optional live effects."""
    def __init__(self, k=1.0, ox=0.0, oy=0.0, s=1.0, size=64, tile_mode='squircle', tile_effects=None):
        self.k, self.ox, self.oy, self.s, self.size = k, ox, oy, s, size
        self.tile_mode, self.tile_effects = tile_mode, tile_effects

    def X(self, v): return self.ox + (self.size / 2 + (v - self.size / 2) * self.s) * self.k
    def Y(self, v): return self.oy + (self.size / 2 + (v - self.size / 2) * self.s) * self.k
    def L(self, v): return v * self.k * self.s
    def TX(self, v): return self.ox + v * self.k

    def tile(self, fill, size=64, R=None, name='tile'):
        R = 0.2237 * size if R is None else R
        x, y, w = self.ox, self.oy, size * self.k
        d = squircle(x, y, w, w, R * self.k) if self.tile_mode == 'squircle' else \
            f'M{f(x)} {f(y)} L{f(x + w)} {f(y)} L{f(x + w)} {f(y + w)} L{f(x)} {f(y + w)} Z'
        k = dict(effects=self.tile_effects) if self.tile_effects else {}
        tool('draw_path', d=d, fill=fill, stroke='none', name=name, **k)

    def hwire(self, y, sw, stroke, name='wire'):
        """A horizontal line from tile edge to tile edge, whatever the glyph scale."""
        x0, x1 = self.TX(0), self.TX(self.size)   # rows sit on the straight part of the edge
        tool('draw_path', d=f'M{f(x0)} {f(self.Y(y))} L{f(x1)} {f(self.Y(y))}', fill='none', stroke=stroke,
             strokeWidth=self.L(sw), cap='butt', name=name)

    def square(self, fill, size=64, name='tile'):
        tool('draw_shape', shape='rectangle', x=self.X(0), y=self.Y(0), width=self.L(size), height=self.L(size),
             fill=fill, stroke='none', name=name)

    def dot(self, cx, cy, r, fill, name='dot'):
        tool('draw_shape', shape='ellipse', x=self.X(cx - r), y=self.Y(cy - r), width=self.L(2 * r),
             height=self.L(2 * r), fill=fill, stroke='none', name=name)

    def ring(self, cx, cy, r_out, sw, stroke, inside, name='ring'):
        """A ring given by its OUTER radius; `inside` paints the counter (hides the spine behind it)."""
        rc = r_out - sw / 2
        tool('draw_shape', shape='ellipse', x=self.X(cx - rc), y=self.Y(cy - rc), width=self.L(2 * rc),
             height=self.L(2 * rc), fill=inside, stroke=stroke, strokeWidth=self.L(sw), name=name)

    def line(self, x1, y1, x2, y2, sw, stroke, cap='round', name='line'):
        tool('draw_path', d=f'M{f(self.X(x1))} {f(self.Y(y1))} L{f(self.X(x2))} {f(self.Y(y2))}',
             fill='none', stroke=stroke, strokeWidth=self.L(sw), cap=cap, name=name)

    def arc(self, cx, cy, r, a0, a1, sw, stroke, name='arc'):
        segs = arc_cubics(self.X(cx), self.Y(cy), self.L(r), a0, a1)
        d = f'M{f(segs[0][0][0])} {f(segs[0][0][1])} ' + ' '.join(
            f'C{f(c1[0])} {f(c1[1])} {f(c2[0])} {f(c2[1])} {f(p3[0])} {f(p3[1])}' for _, c1, c2, p3 in segs)
        tool('draw_path', d=d, fill='none', stroke=stroke, strokeWidth=self.L(sw), cap='round', name=name)

    def path(self, d, fill, name='glyph'):
        tool('draw_path', d=d, fill=fill, stroke='none', name=name)


# ---- themes ------------------------------------------------------------------------------------
THEMES = {
    # tile, primary mark, secondary mark (unread accent), quiet (spine / wires)
    'light': dict(tile=PRUSSIAN, fg=SHEET, accent=SHEET),
    'dark': dict(tile=D_SURFACE, fg=FOG, accent=D_ACCENT),
}
THEMES['app'] = THEMES['light']

# ================================================================================================
# 1. wire: the current favicon, refined
# ================================================================================================
SW = 3.5          # the one stroke weight: titles and the ring
SPINE = SW / 2    # the spine is exactly half of it
SPINE_T = 0.42    # spine = 42 % of the mark colour over the tile, as a solid colour


def wire(pen, th):
    t = THEMES[th]
    pen.tile(t['tile'])
    cx, y1, y2 = 20.5, 22.5, 41.5        # nodes; rows 19 apart, centred on 32
    spine = mix(t['fg'], t['tile'], SPINE_T)
    pen.line(cx, 10, cx, 54, SPINE, spine, name='spine')
    pen.dot(cx, y1, 6, t['accent'], name='unread')
    pen.ring(cx, y2, 6.25, SW, t['fg'], t['tile'], name='read')     # outer 12.5 vs dot 12: optically equal
    pen.line(33, y1, 49.5, y1, SW, t['fg'], name='title-unread')
    pen.line(33, y2, 44, y2, SW, t['fg'], name='title-read')


def wire_small(pen, th):
    """16 px, fitted to the pixel grid: 5 px nodes on a crisp 1 px spine, a 1 px ring, 2 px titles."""
    t = THEMES[th]
    pen.tile(t['tile'], size=16, R=3.6)
    pen.line(4.5, 1, 4.5, 15, 1, mix(t['fg'], t['tile'], 0.45), cap='butt', name='spine')
    pen.dot(4.5, 4.5, 2.5, t['accent'], name='unread')
    pen.ring(4.5, 11.5, 2.5, 1, t['fg'], t['tile'], name='read')
    pen.line(10, 5, 13, 5, 2, t['fg'], name='title-unread')
    pen.line(10, 12, 11, 12, 2, t['fg'], name='title-read')


# ================================================================================================
# 2. arrival: a read ring left open, the unread dispatch arriving in the gap (also a C)
# ================================================================================================
def arrival(pen, th):
    t = THEMES[th]
    pen.tile(t['tile'])
    cx, cy, r, sw, gap = 30, 32, 14.5, 7, 50
    pen.arc(cx, cy, r, gap, 360 - gap, sw, t['fg'], name='read-ring')
    pen.dot(cx + r + 1.5, cy, 5, t['accent'], name='unread')


def arrival_small(pen, th):
    t = THEMES[th]
    pen.tile(t['tile'], size=16, R=3.6)
    pen.arc(7, 8, 4, 55, 305, 2, t['fg'], name='read-ring')
    pen.dot(12, 8, 2, t['accent'], name='unread')


# ================================================================================================
# 3. byline: "C." in Newsreader, the signature at the foot of a dispatch
# ================================================================================================
BYLINE = {
    'light': dict(tile=SHEET, fg=INK, accent=PRUSSIAN),
    'dark': dict(tile=D_SURFACE, fg=FOG, accent=D_ACCENT),
    # app and PWA sets: a white tile vanishes on white home screens, so Prussian with a gold full stop
    'app': dict(tile=PRUSSIAN, fg=SHEET, accent=GOLD),
}
FONT = '/Users/isma/dev/correspondent/ui/node_modules/@fontsource-variable/newsreader/files/newsreader-latin-opsz-normal.woff2'


def glyph(ch, wght, opsz):
    """Outline and ink bounds of one Newsreader glyph at a weight and optical size."""
    from fontTools.ttLib import TTFont
    from fontTools.varLib.instancer import instantiateVariableFont
    from fontTools.pens.boundsPen import BoundsPen
    fnt = instantiateVariableFont(TTFont(FONT), {'wght': wght, 'opsz': opsz})
    gs = fnt.getGlyphSet(); g = gs[fnt.getBestCmap()[ord(ch)]]
    bp = BoundsPen(gs); g.draw(bp)
    return (gs, g), bp.bounds


def glyph_d(gg, k, ox, oy):
    """SVG path data for the glyph scaled by k, y flipped, baseline-left at (ox, oy)."""
    from fontTools.pens.svgPathPen import SVGPathPen
    from fontTools.pens.transformPen import TransformPen
    gs, g = gg
    pen = SVGPathPen(gs)
    g.draw(TransformPen(pen, (k, 0, 0, -k, ox, oy)))
    return pen.getCommands()


def byline_draw(pen, th, size, cap_h, dot_r, gap, wght, R=None):
    t = BYLINE[th]
    pen.tile(t['tile'], size=size, R=R)
    gg, (x0, y0, x1, y1) = glyph('C', wght, 72)
    k = cap_h / (y1 - y0)                       # the C's own height (with overshoot) -> cap_h units
    gw = (x1 - x0) * k
    total = gw + gap + 2 * dot_r
    left = (size - total) / 2 + size * 0.01     # a hair right: the C's open side is light
    base = size / 2 + cap_h / 2 + y0 * k        # vertical centre of the C's ink on the tile centre
    pen.path(glyph_d(gg, pen.L(k), pen.X(left - x0 * k), pen.Y(base)), t['fg'], name='C')
    pen.dot(left + gw + gap + dot_r, base - y0 * k - dot_r - 0.0, dot_r, t['accent'], name='full-stop')


def byline(pen, th):
    byline_draw(pen, th, 64, 34, 3.6, 1.6, 560)


def byline_small(pen, th):
    byline_draw(pen, th, 16, 11, 1.5, 0.6, 700, R=3.6)


# ================================================================================================
# 4. morse: "C" is  — · — ·  on the telegraph wire, set as two lines of  — ·
# ================================================================================================
def morse(pen, th):
    t = THEMES[th]
    pen.tile(t['tile'])
    wire_c = mix(t['fg'], t['tile'], 0.30 if th == 'light' else 0.16)
    for y in (24, 40):
        pen.hwire(y, SPINE, wire_c)
    for y in (24, 40):
        pen.line(16, y, 33, y, 7, t['fg'], name='dash')
        pen.dot(45, y, 4.6, t['accent'], name='dot')


def morse_small(pen, th):
    t = THEMES[th]
    pen.tile(t['tile'], size=16, R=3.6)
    for y in (5.5, 10.5):
        pen.line(4.5, y, 6.5, y, 3, t['fg'], name='dash')
        pen.dot(11.5, y, 1.5, t['accent'], name='dot')


OPTIONS = {
    'wire': (wire, wire_small),
    'arrival': (arrival, arrival_small),
    'byline': (byline, byline_small),
    'morse': (morse, morse_small),
}


# ---- build ----------------------------------------------------------------------------------------
def fresh(name, w):
    os.makedirs(TMP, exist_ok=True)
    p = f'{TMP}/{name}.svg'
    open(p, 'w').write(f'<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{w}" viewBox="0 0 {w} {w}"></svg>')
    tool('open_file', path=p)


def build(opt):
    full, small = OPTIONS[opt]
    out = f'{B}/exports/{opt}'
    os.makedirs(out, exist_ok=True)
    for th in ('light', 'dark'):
        name = f'{opt}-{th}'
        fresh(name, 1024)
        full(Pen(16), th)
        tool('save_file', path=f'{S}/{name}.vectorcraft')
        tool('export', path=f'{out}/{name}.svg', format='svg')
        for px in (180, 512, 1024):
            tool('export', path=f'{out}/{name}-{px}.png', format='png', scale=px / 1024)
        name_s = f'{opt}-small-{th}'
        fresh(name_s, 16)
        small(Pen(1), th)
        tool('save_file', path=f'{S}/{name_s}.vectorcraft')
        tool('export', path=f'{out}/{name}-small.svg', format='svg')
        for px in (16, 32):
            tool('export', path=f'{out}/{name}-{px}.png', format='png', scale=px / 16)
    print('ok', opt)


# ---- app / PWA sets (light tile; byline uses its 'app' theme) -----------------------------------------
# glyph scale for the maskable icon: every glyph point inside the 40 %-radius safe circle with a margin
# (checked by check_maskable below; morse's wires are background and are meant to run off the tile)
MASK_S = {'wire': 0.94, 'byline': 0.95, 'morse': 1.0}
APPS = ('wire', 'byline', 'morse')
# macOS Big Sur grid: an 824 pt tile inset 100 pt on the 1024 canvas, with a soft drop shadow below
MAC_SHADOW = [{'effect': 'stylize.dropShadow',
               'params': {'x': 0, 'y': 12, 'blur': 14, 'opacity': 32, 'mode': 'multiply', 'color': '#000000'}}]


def app(opt, debug_dir=None):
    full, small = OPTIONS[opt]
    out = f'{B}/exports/{opt}/app'
    os.makedirs(out, exist_ok=True)
    # 1. favicon: the pixel-fitted small design
    fresh(f'{opt}-favicon', 16); small(Pen(1, size=16), 'app')
    tool('save_file', path=f'{S}/{opt}-app-favicon.vectorcraft')
    tool('export', path=f'{out}/favicon.svg', format='svg')
    tool('export', path=f'{out}/favicon-32.png', format='png', scale=2)
    # 2. apple-touch-icon: full-bleed square, glyph at its normal size
    fresh(f'{opt}-apple-touch', 1024); full(Pen(16, tile_mode='square'), 'app')
    tool('save_file', path=f'{S}/{opt}-app-touch.vectorcraft')
    tool('export', path=f'{out}/apple-touch-icon.png', format='png', scale=180 / 1024)
    # 3. purpose "any": the rounded tile, transparent corners
    fresh(f'{opt}-icon', 1024); full(Pen(16), 'app')
    tool('save_file', path=f'{S}/{opt}-app-icon.vectorcraft')
    for px in (192, 512):
        tool('export', path=f'{out}/icon-{px}.png', format='png', scale=px / 1024)
    # 4. purpose "maskable": full bleed, glyph inside the safe zone
    fresh(f'{opt}-maskable', 1024); full(Pen(16, s=MASK_S[opt], tile_mode='square'), 'app')
    tool('save_file', path=f'{S}/{opt}-app-maskable.vectorcraft')
    tool('export', path=f'{out}/icon-maskable-512.png', format='png', scale=0.5)
    if debug_dir:  # the same document with the safe-zone circle on top, exported elsewhere, then removed
        cid = tool('draw_shape', shape='ellipse', x=512 - 409.6, y=512 - 409.6, width=819.2, height=819.2,
                   fill='none', stroke='#FF2D55', strokeWidth=6, name='safe-zone')['id']
        tool('export', path=f'{debug_dir}/{opt}-maskable-safezone.png', format='png', scale=0.5)
        tool('undo')
    # 5. macOS: inset tile with the standard shadow
    fresh(f'{opt}-mac', 1024); full(Pen(824 / 64, 100, 100, tile_effects=MAC_SHADOW), 'app')
    tool('save_file', path=f'{S}/{opt}-app-mac.vectorcraft')
    tool('export', path=f'{out}/mac-icon-1024.png', format='png', scale=1)
    print('ok app', opt)


if __name__ == '__main__':
    args = sys.argv[1:]
    if args[:1] == ['app']:
        dbg = os.environ.get('VC_DEBUG_DIR')
        for o in (args[1:] or APPS):
            app(o, dbg)
        sys.exit()
    for o in (sys.argv[1:] or OPTIONS):
        build(o)
