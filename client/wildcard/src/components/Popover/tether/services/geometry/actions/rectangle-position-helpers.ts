import { createRectangle, Rectangle } from '../../../models/geometry/rectangle'
import { Flipping, Position } from '../../../models/tether-models'
import { POSITION_VARIANTS } from '../constants'

/**
 * Returns all possible positions (top, top-left, left, right, ..etc) based on
 * initial position and flipping settings.
 */
export function getPositions(position: Position, flipping: Flipping): Position[] {
    const positions = [position, POSITION_VARIANTS[position].opposite]

    if (flipping === Flipping.all) {
        positions.push(Position.topStart, Position.rightStart, Position.bottomStart, Position.leftStart)
    }

    return positions
}

export function getRoundedElement(element: Rectangle): Rectangle {
    return createRectangle(
        Math.floor(element.left),
        Math.floor(element.top),
        Math.floor(element.width),
        Math.floor(element.height)
    )
}

/**
 * Returned bounded (constrained element) element. Returns null in case if element hasn't been
 * constrained during position calculations.
 *
 * @param element - constrained tooltip element
 * @param originalElement - original sized tooltip element
 */
export function getElementBounds(
    element: Rectangle,
    originalElement: Rectangle,
    elementConstraint: Rectangle
): Rectangle | null {
    if (element.width < originalElement.width || element.height < originalElement.height) {
        return elementConstraint
    }

    return null
}

export function isElementVisible(element: Rectangle): boolean {
    return element.width * element.height >= 1
}
