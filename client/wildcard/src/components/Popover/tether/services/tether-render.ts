import {
    getScrollPositions,
    isVisible,
    setMaxSize,
    setScrollPositions,
    setStyle,
    setTransform,
    setVisibility,
} from './tether-browser'
import { getLayout } from './tether-layout'
import { getState } from './tether-state'
import { Tether } from './types'

/**
 * Main entry point for tooltip element position calculations. It mutates tooltip
 * DOM element (set visibility, width, height, top, left) properties.
 *
 * @param tether - settings tooltip information.
 * @param eventTarget - event target element event of which has triggered tooltip
 * position update.
 * @param preserveSize - tells render function do not removing max width|height sizes before
 * size measuring.
 */
export function render(tether: Tether, eventTarget: HTMLElement | null, preserveSize?: boolean): void {
    const positions = getScrollPositions(tether.element)

    if (!positions.points.has(eventTarget as Element) && !preserveSize) {
        setMaxSize(tether.element, null)
    }

    // Restore visibility for correct measure in layout service
    setVisibility(tether.element, true)
    setVisibility(tether.marker ?? null, true)

    const layout = getLayout(tether)
    const state = getState(layout)

    if (state === null || !isVisible(tether.target)) {
        setVisibility(tether.element, false)
        setVisibility(tether.marker ?? null, false)

        return
    }

    setTransform(tether.element, 0, state.elementOffset)
    setTransform(tether.marker ?? null, state.markerAngle, state.markerOffset)

    if (!positions.points.has(eventTarget as Element)) {
        const bounds = state.elementBounds ?? state.constrainedElement
        const elementMaxWidth = Math.min(bounds.width, state.constrainedElement.width)

        // Bound width of the floating element
        setStyle(tether.element, 'max-width', `${elementMaxWidth}px`)

        const nextElement = tether.element.getBoundingClientRect()

        // If height has been changed by bounding max-width it might change the whole
        // position calculation. In this case we need to run render algorithm again
        // to take into account a new width of the floating element.
        if (layout.element.height !== nextElement.height) {
            render(tether, eventTarget, true)
        } else {
            const elementMaxHeight = Math.min(bounds.height, state.constrainedElement.height)

            setStyle(tether.element, 'max-height', `${elementMaxHeight}px`)
        }

        // Restore containers scroll positions
        setScrollPositions(positions)
    }
}
