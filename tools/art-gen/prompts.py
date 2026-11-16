#!/usr/bin/env python3
"""The words sent to the image model: one shared style preamble per kind of
face, then one subject line per original (docs/ART.md).

    python3 tools/art-gen/prompts.py french spades-ace    print one prompt
    python3 tools/art-gen/prompts.py reference-board      print the board's six

The preamble decides the look. It names the six colours by hex, one line
weight, flat fills, blocky front-facing figures, the empty corners where the
packer stamps the index, and a plain paper ground to the edges (the packer
draws the edge and cuts the corner itself, so the model must not). Words the
style forbids appear only in the sentence that begins "Do not draw:".
"""
import os, sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import spec, inventory  # noqa: E402

COLOURS = 'paper %s, navy ink %s, lavender %s, cyan %s, pink %s, yellow %s' % tuple(
    c.upper() for c in (spec.PAPER, spec.INK, spec.LAVENDER, spec.CYAN, spec.PINK, spec.YELLOW))

STYLE = (
    'Palette: use only these six colours, each as one flat, even, solid fill: %s. Nothing else: no white, '
    'no black, no grey, no tints, no in-between colours. '
    'Line: every outline is navy ink and the same thickness everywhere in the picture, a bold line about '
    'one sixtieth of the image width, with square line ends and sharp mitred corners. Every coloured shape '
    'has this ink outline. '
    'Shapes: blocky and angular, built from straight lines; corners are sharp or cut off at 45 degrees. '
    'Circles only for things that are truly round, such as a coin, a sun, a moon or a dot. Figures face the '
    'viewer straight on, symmetrical where they can be, simplified like a bold pictogram icon set. '
    'Each area is a single flat colour.' % COLOURS)

LAYOUT = {
    'card': 'Layout: the face of a portrait playing card seen straight on, filling the whole image. The '
            'background is plain paper %s right to the edges of the image; do not draw the card\'s outline. '
            'Keep a margin of plain paper about one eighth of the width on every side; the top and bottom will '
            'be trimmed slightly. Keep the top-left and bottom-right corners empty paper (a square about one '
            'quarter of the width each), where the rank is added afterwards, and keep the top-right corner '
            'empty too.' % spec.PAPER.upper(),
    'hanafuda': 'Layout: the face of a Japanese hanafuda flower card seen straight on, filling the whole image. '
                'The background is plain paper %s right to the edges of the image; do not draw the card\'s '
                'outline. Keep a margin of plain paper about one tenth of the width on every side, and keep the '
                'top-right corner empty.' % spec.PAPER.upper(),
    'tile': 'Layout: the face of a mahjong tile seen straight on, filling the whole image. The background is '
            'plain paper %s right to the edges of the image; do not draw the tile\'s outline or its thickness. '
            'Keep a margin of plain paper about one eighth of the width on every side, and keep the top-right '
            'corner empty.' % spec.PAPER.upper(),
    'die': 'Layout: one face of a six-sided die seen straight on, filling the whole image edge to edge. The '
           'face is plain paper %s right to the edges of the image; do not draw the die\'s outline. Keep a '
           'margin of plain paper about one sixth of the width on every side.' % spec.PAPER.upper(),
    'poly': 'Layout: one flat polygon seen straight on, centred, on plain paper %s right to the edges of the '
            'image, with a margin of plain paper about one eighth of the width on every side. The polygon '
            'is blank inside: one flat fill, no number, no marks.' % spec.PAPER.upper(),
}

WHAT = {'card': 'one playing card', 'hanafuda': 'one hanafuda card', 'tile': 'one mahjong tile',
        'die': 'one die face', 'poly': 'one die shape'}

DONT_START = 'Do not draw:'
DONT = ('gradients, shading, shadows, highlights, glow, gloss, texture, grain, noise, blur, halftone dots, '
        'lines of varying thickness, rounded corners, perspective or 3D, realistic or photographic rendering, '
        'sketchy hand-drawn lines, signatures, watermarks, logos, a border or frame around the picture')
NO_TEXT = 'letters, numbers or words of any kind'
NO_TEXT_TILE = 'Latin letters, Arabic numerals or words, other than the Chinese characters named in the subject'

# Words the style forbids; tests check none appears outside DONT.
FORBIDDEN = ['gradient', 'shading', 'shadow', 'highlight', 'glow', 'gloss', 'texture', 'grain', 'blur',
             'halftone', 'rounded', 'perspective', '3d', 'realistic', 'photographic', 'sketch', 'watermark',
             'bevel', 'airbrush', 'soft']


def preamble(kind):
    no_text = NO_TEXT_TILE if kind == 'tile' else NO_TEXT
    return 'Artwork for %s in a strict flat graphic style for the chat app Churr. %s %s %s %s; %s.' % (
        WHAT[kind], STYLE, LAYOUT[kind], DONT_START, DONT, no_text)


# --- suit marks --------------------------------------------------------------

MARK = {
    # French
    'spades': 'spade (a pointed leaf shape on a short stem, made of straight facets) in solid navy ink',
    'hearts': 'heart (built from straight facets, with a sharp point) in solid pink with an ink outline',
    'diamonds': 'diamond (a square stood on its point, slightly taller than wide) in solid pink with an ink outline',
    'clubs': 'club (three square-cut lobes on a short stem, made of straight facets) in solid navy ink',
    # Tarot
    'wands': 'wand (a straight wooden staff with two small leaf shapes sprouting from it) in lavender',
    'cups': 'cup (a goblet with a wide angular bowl on a stem and a flat foot) in pink',
    'swords': 'sword (a straight double-edged blade in cyan with a crossguard and grip in yellow)',
    'pentacles': 'pentacle (a round yellow coin with a five-pointed star outlined inside it)',
    # Latin
    'coins': 'coin (a round yellow disc with a simple eight-pointed star inside it)',
    'batons': 'baton (a thick knotted wooden club, wider at the top) in lavender',
    # German and Swiss
    'acorns': 'acorn (a lavender nut under an ink cap of small squares, on a short stem)',
    'leaves': 'leaf (pointed, built from straight facets, with an ink midrib and stem) in cyan',
    'gheart': 'heart (built from straight facets, with a sharp point) in solid pink with an ink outline',
    'bells': 'bell (a small round yellow sleigh bell with a slot across its lower half and a loop on top)',
    'shields': 'shield (heraldic, with a flat top and a pointed base, divided into cyan and paper halves)',
    'roses': 'rose (a front-facing five-petalled pink flower with a yellow centre, petals cut square at the tips)',
}
PLURAL = {'spades': 'spades', 'hearts': 'hearts', 'diamonds': 'diamonds', 'clubs': 'clubs', 'wands': 'wands',
          'cups': 'cups', 'swords': 'swords', 'pentacles': 'pentacles', 'coins': 'coins', 'batons': 'batons',
          'acorns': 'acorns', 'leaves': 'leaves', 'gheart': 'hearts', 'bells': 'bells', 'shields': 'shields',
          'roses': 'roses'}
# Long marks that cross in a lattice on Latin-suited and Marseille pip cards.
LONG = {'swords', 'wands', 'batons'}

WORDS = ['zero', 'one', 'two', 'three', 'four', 'five', 'six', 'seven', 'eight', 'nine', 'ten']

FRENCH_LAYOUT = {
    2: 'one at the top centre and one at the bottom centre',
    3: 'in one vertical column at the top, middle and bottom of the centre line',
    4: 'one near each corner of the picture area, in two columns of two',
    5: 'one near each corner of the picture area and one in the centre',
    6: 'in two columns of three',
    7: 'in two columns of three, with one more on the centre line between the top and middle rows',
    8: 'in two columns of three, with one more on the centre line between each pair of rows',
    9: 'in two columns of four, with one more in the centre',
    10: 'in two columns of four, with one more on the centre line between the top two rows and one between the '
        'bottom two rows',
}


def pips(n, suit):
    marks = '%s %s' % (WORDS[n], PLURAL[suit])
    return 'The %s of %s: %s, each a %s, arranged %s; the marks in the lower half point downwards.' % (
        WORDS[n], PLURAL[suit], marks, MARK[suit], FRENCH_LAYOUT[n])


def latin_pips(n, suit, deck):
    if suit in LONG:
        how = ('crossed diagonally in a lattice, as on old %s cards, with one more upright in the centre'
               % deck) if n % 2 else ('crossed diagonally in a lattice, as on old %s cards' % deck)
        if n == 2:
            how = 'crossed in an X, as on old %s cards' % deck
        if n == 3:
            how = 'two crossed in an X with one upright through the crossing, as on old %s cards' % deck
    else:
        how = FRENCH_LAYOUT[n] if n in FRENCH_LAYOUT else 'in a symmetrical block'
    return 'The %s of %s: %s %s, each a %s, arranged %s.' % (WORDS[n], PLURAL[suit], WORDS[n], PLURAL[suit], MARK[suit], how)


# --- French ------------------------------------------------------------------

COURT_FR = {
    'jack': 'a young man in a flat cap with a feather, holding a halberd upright',
    'queen': 'a queen in a stepped crown, holding a single flower',
    'king': 'a bearded king in a stepped crown, holding a sword upright',
}


def french(fid):
    if fid == 'back':
        return ('The back of the card: the whole picture is solid lavender %s to every edge, covered with an even '
                'grid of small round navy ink dots in straight rows and columns, about twenty dots across. '
                'Nothing else. (For this face the background is lavender, not paper, and the corner rule does not '
                'apply: the dots run into every corner.)' % spec.LAVENDER)
    if fid.startswith('joker-'):
        tone = {'red': 'pink and lavender, with pink as the main colour',
                'black': 'navy ink and cyan, with navy ink as the main colour',
                'white': 'paper with ink outlines only, and small yellow accents'}[fid.split('-')[1]]
        return ('A joker: a front-facing jester standing with arms out, in a three-pointed cap with a square bell on '
                'each point, a diamond-patterned tunic and pointed shoes, coloured in %s.' % tone)
    suit, rank = fid.split('-', 1)
    if rank == 'ace':
        extra = ' inside a simple stepped ornament of straight ink lines' if suit == 'spades' else ''
        return 'The ace of %s: one large %s in the centre of the card%s.' % (PLURAL[suit], MARK[suit], extra)
    if rank.isdigit():
        return pips(int(rank), suit)
    colour = 'pink and yellow' if suit in ('hearts', 'diamonds') else 'navy ink and cyan'
    return ('The %s of %s: %s, drawn double-ended as on a classic court card (the half figure in the top half, '
            'mirrored upside down in the bottom half, meeting at a straight diagonal line), clothes in %s with '
            'lavender details, and a small %s beside the figure in each half.' % (
                rank, PLURAL[suit], COURT_FR[rank], colour, MARK[suit].split(' (')[0]))


# --- Tarot -------------------------------------------------------------------

MAJORS = [
    'The Fool: a young traveller striding forward with a bundle on a stick over one shoulder and a small dog at '
    'his heels, at the edge of a stepped cliff, a yellow sun behind.',
    'The Magician: a figure behind a square table, one arm raised holding a short wand, the other pointing down; '
    'on the table a cup, a sword and a coin; a sideways figure-eight above the head.',
    'The High Priestess: a seated woman in a tall stepped headdress between two square pillars, one navy and one '
    'paper, holding a closed scroll, a crescent moon at her feet.',
    'The Empress: a crowned woman seated on a cushioned throne in a field of wheat, holding a sceptre, a shield '
    'with a heart beside her.',
    'The Emperor: a bearded ruler on a square stone throne carved with ram heads, holding a sceptre and an orb, '
    'mountains of stepped blocks behind.',
    'The Hierophant: a robed figure in a three-tiered crown seated between two pillars, one hand raised in '
    'blessing, two small kneeling figures before him, two crossed keys at his feet.',
    'The Lovers: two figures standing side by side facing out, a winged figure with spread wings above them, a '
    'yellow sun at the top.',
    'The Chariot: a crowned figure standing in a square chariot under a canopy of stars, drawn by two sphinxes, '
    'one navy and one paper.',
    'Strength: a woman in a flowered crown calmly holding open the jaws of a lion, a sideways figure-eight above '
    'her head.',
    'The Hermit: a hooded old man with a long staff holding up a lantern that contains a six-pointed yellow star.',
    'Wheel of Fortune: a large round wheel with eight spokes in the centre, a sphinx on top, a figure climbing '
    'one side, clouds in the four corners left as plain paper.',
    'Justice: a crowned figure seated between two pillars, holding an upright sword in one hand and balanced '
    'scales in the other.',
    'The Hanged Man: a man hanging upside down by one ankle from a T-shaped wooden beam, the free leg bent behind '
    'the other, arms behind his back, a yellow ring around his head.',
    'Death: a skeleton in navy ink armour on a paper-coloured horse, carrying a banner with a five-petalled rose, a yellow sun '
    'setting between two towers behind.',
    'Temperance: a winged angel standing with one foot on land and one in a pool, pouring water from one cup into '
    'another.',
    'The Devil: a horned figure with bat wings squatting on a square block, one hand raised, two small chained '
    'figures standing below.',
    'The Tower: a tall square stone tower on a rocky peak, struck at the top by a zigzag bolt of yellow lightning '
    'that knocks off its crown-shaped roof, pink flames from the windows, two small figures falling, small square '
    'sparks in the air.',
    'The Star: a kneeling figure pouring water from two jugs, one into a pool and one onto the land, a large '
    'eight-pointed yellow star above with seven small stars around it.',
    'The Moon: a large crescent moon with a face in profile in a full round moon, two square towers at either side, '
    'a dog and a wolf howling, a crayfish climbing from a pool, a path running between them.',
    'The Sun: a large round yellow sun with a calm face and straight rays, a child riding a paper-coloured horse below in '
    'front of a wall of square blocks with sunflowers.',
    'Judgement: an angel with a trumpet and a banner bearing a cross leaning from clouds above, figures rising '
    'from open square boxes below with arms raised.',
    'The World: a dancing figure inside an upright oval wreath tied with ribbons at top and bottom, an angel, an '
    'eagle, a bull and a lion in the four corners, each in a small square frame.',
]

COURT_TAROT = {
    'page': 'a young page standing, holding up %s and looking at it',
    'knight': 'a knight in angular armour on a horse, both facing the viewer, holding %s',
    'queen': 'a queen seated on a throne, holding %s',
    'king': 'a king seated on a square throne, holding %s',
}


def tarot(fid):
    if fid.startswith('major-'):
        return MAJORS[int(fid.split('-')[1])]
    suit, rank = fid.split('-', 1)
    if rank == 'ace':
        return ('The ace of %s: a single large %s held upright by a hand coming out of a stepped cloud at the side.'
                % (PLURAL[suit], MARK[suit]))
    if rank.isdigit():
        return latin_pips(int(rank), suit, 'Tarot de Marseille') + ' A small leaf or flower ornament of straight ' \
            'lines may fill gaps between the marks.'
    return 'The %s of %s: %s, with lavender and %s clothing.' % (
        rank, PLURAL[suit], COURT_TAROT[rank] % ('one large ' + MARK[suit]),
        {'wands': 'yellow', 'cups': 'pink', 'swords': 'cyan', 'pentacles': 'yellow'}[suit])


# --- Latin (Spanish and Italian share the figures) ---------------------------

COURT_LATIN = {
    'sota': 'the knave: a young page standing, holding %s',
    'caballo': 'the knight: a rider on a horse, both facing the viewer, holding %s',
    'rey': 'the king: a bearded king standing in a crown and a long robe, holding %s',
}


def latin(fid):
    suit, rank = fid.split('-', 1)
    if rank == 'ace':
        return 'The ace of %s: one large %s in the centre of the card, framed by a simple ring of straight lines.' % (
            PLURAL[suit], MARK[suit])
    if rank.isdigit():
        return latin_pips(int(rank), suit, 'Spanish and Italian')
    return 'The %s of %s, a Latin-suited court card: %s, full figure, standing on a plain floor line.' % (
        COURT_LATIN[rank].split(':')[0][4:], PLURAL[suit], COURT_LATIN[rank].split(': ', 1)[1] % ('one large ' + MARK[suit]))


# --- German and Swiss --------------------------------------------------------

def german_pips(n, suit):
    lay = {6: 'in two columns of three', 7: 'in two columns of three with one more in the centre',
           8: 'in two columns of four', 9: 'in three rows of three'}
    if n == 10:
        return ('The ten of %s: ten %s, each a %s, in two columns of four with two more on the centre line.'
                % (PLURAL[suit], PLURAL[suit], MARK[suit]))
    return 'The %s of %s: %s %s, each a %s, arranged %s.' % (WORDS[n], PLURAL[suit], WORDS[n], PLURAL[suit], MARK[suit], lay[n])


GERMAN_COURT = {
    'unter': 'the unter: a soldier in a doublet and plumed hat, standing, holding %s low in front of him',
    'ober': 'the ober: an officer in a doublet and plumed hat, standing, holding %s raised above his head',
    'king': 'the king: a bearded king on a square throne in a stepped crown, holding a sceptre, with %s beside him',
    'under': 'the under: a soldier in a doublet and plumed hat, standing, holding %s low in front of him',
}


def german(fid, deck='German'):
    suit, rank = fid.split('-', 1)
    if rank == 'ace':
        return ('The ace (deuce) of %s: two large %s side by side in the centre, each a %s, with a small lion '
                'standing on its hind legs behind them, as on old %s cards.' % (PLURAL[suit], PLURAL[suit], MARK[suit], deck))
    if rank == 'banner':
        return ('The banner of %s: a square flag on an upright pole, waving, with one large %s on the flag.'
                % (PLURAL[suit], MARK[suit]))
    if rank.isdigit():
        return german_pips(int(rank), suit)
    return 'The %s of %s, a %s-suited court card: %s, full figure.' % (
        GERMAN_COURT[rank].split(':')[0][4:], PLURAL[suit], deck, GERMAN_COURT[rank].split(': ', 1)[1] % ('one ' + MARK[suit]))


def swiss(fid):
    return german(fid, 'Swiss')


# --- Hanafuda ----------------------------------------------------------------

POETRY = 'a pink poetry ribbon hanging in a gentle vertical zigzag, with three small ink squares down its middle'
RED_RIBBON = 'a plain pink ribbon hanging in a gentle vertical zigzag'
BLUE_RIBBON = 'a plain lavender ribbon hanging in a gentle vertical zigzag'
PLANT = {
    'pine': 'January: a pine tree with flat-topped clumps of needles in cyan on a stepped ink trunk',
    'plum': 'February: a plum branch with small round pink five-petalled blossoms on a zigzag ink branch',
    'cherry': 'March: cherry blossom, clusters of pink five-petalled flowers with notched tips on dark branches',
    'wisteria': 'April: wisteria, long hanging lavender flower clusters made of small squares, hanging from above',
    'iris': 'May: irises, tall pointed cyan leaves with lavender iris flowers made of angular petals',
    'peony': 'June: peonies, large pink peony flowers of layered angular petals with cyan leaves',
    'clover': 'July: bush clover, arching ink stems with small pink flowers and cyan oval leaves',
    'pampas': 'August: pampas grass, a low stepped hill of navy ink with paper-coloured grass plumes along its top edge',
    'chrysanthemum': 'September: chrysanthemums, round yellow flowers of many straight petals with cyan leaves',
    'maple': 'October: maple, branches with pointed pink maple leaves',
    'willow': 'November: willow, long hanging cyan willow fronds',
    'paulownia': 'December: paulownia, three tall lavender flower spikes rising above broad cyan leaves',
}
HANA_SPECIAL = {
    'pine-bright': 'a crane in paper colour with ink outlines and a pink crown standing tall in front of a large round pink sun, under the pine',
    'plum-animal': 'a small cyan bush warbler perched on the plum branch',
    'cherry-bright': 'a pink and lavender curtain with a stepped pattern hanging across the lower half, under the '
                     'blossom',
    'wisteria-animal': 'a cuckoo flying across a thin yellow crescent moon, beside the wisteria',
    'iris-animal': 'a zigzag wooden plank bridge crossing the lower half in front of the irises',
    'peony-animal': 'two butterflies with angular wings in yellow and lavender above the peonies',
    'clover-animal': 'a wild boar standing among the clover, facing the viewer',
    'pampas-bright': 'a large round full moon in yellow rising behind the hill, the sky above in solid navy ink',
    'pampas-animal': 'three wild geese flying in a V across a solid pink sky above the hill',
    'chrysanthemum-animal': 'a shallow pink sake cup on a stand, plain with no writing, among the chrysanthemums',
    'maple-animal': 'a deer standing among the maple leaves, facing the viewer',
    'willow-bright': 'a figure in a court robe holding a yellow umbrella, a small frog jumping at the willow\'s foot, '
                     'and a stream of ink stripes',
    'willow-animal': 'a swallow diving through the willow fronds',
    'willow-chaff1': 'a large drum with zigzag lightning in yellow and pink striking down, the willow above',
    'paulownia-bright': 'a phoenix with spread tail feathers in pink, yellow and cyan, standing below the paulownia',
    'paulownia-chaff3': 'the plant alone, with a solid yellow band across the foot of the card',
}
RIBBON = {'pine': POETRY, 'plum': POETRY, 'cherry': POETRY, 'wisteria': RED_RIBBON, 'iris': RED_RIBBON,
          'clover': RED_RIBBON, 'willow': RED_RIBBON, 'peony': BLUE_RIBBON, 'chrysanthemum': BLUE_RIBBON,
          'maple': BLUE_RIBBON}


def hanafuda(fid):
    month, card = fid.split('-', 1)
    plant = PLANT[month]
    if fid in HANA_SPECIAL:
        thing = HANA_SPECIAL[fid]
    elif card == 'ribbon':
        thing = RIBBON[month] + ' across the plant'
    elif card == 'chaff1':
        thing = 'the plant alone, filling the card from the bottom'
    elif card == 'chaff2':
        thing = 'the plant alone, filling the card from the top, arranged differently from the other plain card'
    else:
        thing = 'the plant alone'
    kind = {'bright': 'bright', 'animal': 'animal', 'ribbon': 'ribbon'}.get(card, 'plain')
    return 'A %s card of the %s month. %s. In front: %s.' % (kind, month, plant, thing)


# --- Mahjong -----------------------------------------------------------------

NUMERALS = ['一', '二', '三', '四', '五', '六', '七', '八', '九']
DOT_LAYOUT = {
    1: 'one large round dot in the centre, drawn as a ring of cyan around a pink centre with a small flower of '
       'straight petals',
    2: 'two round dots one above the other, one cyan and one pink',
    3: 'three round dots on a diagonal from top-left to bottom-right: cyan, pink, navy',
    4: 'four round dots in a square, two cyan and two navy diagonally opposite',
    5: 'four round dots in a square with a pink one in the centre',
    6: 'six round dots: two cyan in a row at the top and four pink in two rows below',
    7: 'seven round dots: three cyan on a diagonal at the top and four pink in a square below',
    8: 'eight round dots in two columns of four, all navy',
    9: 'nine round dots in three rows of three: cyan, pink, navy rows',
}
BAMBOO_LAYOUT = {
    1: 'a bird standing on a bamboo stalk, facing the viewer, with spread tail feathers in cyan, pink and yellow',
    2: 'two upright bamboo sticks one above the other in the centre',
    3: 'three bamboo sticks: one at the top centre and two side by side below',
    4: 'four bamboo sticks in two columns of two',
    5: 'four bamboo sticks in two columns of two with a pink one upright in the centre',
    6: 'six bamboo sticks in two rows of three',
    7: 'seven bamboo sticks: one pink at the top centre and six in two rows of three below',
    8: 'eight bamboo sticks: four on the top row and four below, the middle pairs leaning together into two Vs',
    9: 'nine bamboo sticks in three rows of three, the middle column pink',
}
MAHJONG_HONOURS = {
    'wind-east': 'the east wind: the single Chinese character 東 in large bold navy ink strokes, centred',
    'wind-south': 'the south wind: the single Chinese character 南 in large bold navy ink strokes, centred',
    'wind-west': 'the west wind: the single Chinese character 西 in large bold navy ink strokes, centred',
    'wind-north': 'the north wind: the single Chinese character 北 in large bold navy ink strokes, centred',
    'dragon-red': 'the red dragon: the single Chinese character 中 in large bold pink strokes with an ink outline, '
                  'centred',
    'dragon-green': 'the green dragon: the single Chinese character 發 in large bold cyan strokes with an ink '
                    'outline, centred',
    'dragon-white': 'the white dragon: an empty upright rectangular frame in a bold lavender line with an ink '
                    'outline, centred, nothing inside it',
    'flower-plum': 'the plum flower tile: a single plum branch in navy ink with five round pink blossoms, centred',
    'flower-orchid': 'the orchid flower tile: an orchid plant with long arching cyan leaves and two lavender '
                     'flowers, centred',
    'flower-chrysanthemum': 'the chrysanthemum flower tile: one large round yellow chrysanthemum of straight petals '
                            'with two cyan leaves, centred',
    'flower-bamboo': 'the bamboo flower tile: two upright cyan bamboo stalks with joints and pointed leaves, centred',
    'season-spring': 'the spring tile: a sprouting seedling with two cyan leaves and a small pink blossom, centred',
    'season-summer': 'the summer tile: a round yellow sun with straight rays over a lotus flower in pink, centred',
    'season-autumn': 'the autumn tile: a bundle of yellow wheat stalks with two falling pink maple leaves, centred',
    'season-winter': 'the winter tile: a lavender snowflake of straight lines over a stepped navy hill, centred',
}


def mahjong(fid):
    if fid in MAHJONG_HONOURS:
        return 'A mahjong tile, ' + MAHJONG_HONOURS[fid] + '.'
    suit, n = fid.split('-')
    n = int(n)
    if suit == 'dots':
        return 'A mahjong tile, the %s of dots (circles): %s.' % (WORDS[n], DOT_LAYOUT[n])
    if suit == 'bamboo':
        return ('A mahjong tile, the %s of bamboo: %s. A bamboo stick is a narrow upright cyan bar with a navy joint '
                'across its middle and square ends.' % (WORDS[n], BAMBOO_LAYOUT[n]))
    return ('A mahjong tile, the %s of characters: the Chinese numeral %s in bold navy ink in the upper half and the '
            'character 萬 in bold pink strokes with an ink outline in the lower half, both centred.' % (
                WORDS[n], NUMERALS[n - 1]))


# --- Dice --------------------------------------------------------------------

D6 = {
    1: 'one large square pip in pink with an ink outline, in the centre',
    2: 'two square navy pips on a diagonal, top-left and bottom-right',
    3: 'three square navy pips on a diagonal from top-left to bottom-right',
    4: 'four square navy pips, one near each corner',
    5: 'four square navy pips, one near each corner, and one in the centre',
    6: 'six square navy pips in two columns of three',
}
POLY_SUBJECT = {
    'd4': 'the face of a four-sided die: an equilateral triangle standing on its base, filled cyan',
    'd8': 'the face of an eight-sided die: an equilateral triangle standing on its base, filled yellow',
    'd10': 'the face of a ten-sided die: a kite shape, point up and point down, wider above the middle, filled pink',
    'd12': 'the face of a twelve-sided die: a regular pentagon standing on its base, filled lavender',
    'd20': 'the face of a twenty-sided die: an equilateral triangle standing on its base, filled lavender, with a '
           'second, smaller triangle outline inside it, point down, its corners touching the middle of each side',
    'd100': 'the face of a percentile die: a kite shape, point up and point down, filled yellow, with a second, '
            'smaller kite outline inside it',
    'dn': 'the face of a die with an unusual number of sides: a regular hexagon standing on a flat side, filled '
          'paper with only its ink outline',
}


def dice_cube(fid):
    n = int(fid.split('-')[1])
    return 'A die face showing %s: %s. Pips are squares, never circles.' % (WORDS[n], D6[n])


def dice_poly(fid):
    return 'A die shape, %s. It fills about three quarters of the image width.' % POLY_SUBJECT[fid]


SUBJECT = {'french': french, 'tarot': tarot, 'latin': latin, 'german': german, 'swiss': swiss,
           'hanafuda': hanafuda, 'mahjong': mahjong, 'dice-cube': dice_cube, 'dice-poly': dice_poly}

# The reference board: six faces that show the look across the sets. Each is
# a face of its set and is kept as that face's original.
REFERENCE_BOARD = [('french', 'spades-ace'), ('french', 'hearts-queen'), ('tarot', 'major-16'),
                   ('hanafuda', 'pine-bright'), ('mahjong', 'flower-plum'), ('dice-cube', 'd6-5')]


def prompt(sheet, original):
    s = spec.BY_NAME[sheet]
    return preamble(s.kind) + ' Subject: ' + SUBJECT[sheet](original)


def jobs(set_name, reg=None):
    """[(sheet, original id, prompt)] for a set or the reference board."""
    if set_name == 'reference-board':
        return [(sh, fid, prompt(sh, fid)) for sh, fid in REFERENCE_BOARD]
    faces = inventory.build(reg or inventory.load_registry())[set_name]
    return [(set_name, o, prompt(set_name, o)) for o in inventory.originals(faces)]


def main():
    args = sys.argv[1:]
    if not args:
        sys.exit('usage: prompts.py <set>|reference-board [face id]')
    for sh, fid, p in jobs(args[0]):
        if len(args) > 1 and fid != args[1]:
            continue
        print('== %s/%s\n%s\n' % (sh, fid, p))


if __name__ == '__main__':
    main()
