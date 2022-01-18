import { action } from '@storybook/addon-actions'
import { storiesOf } from '@storybook/react'
import React from 'react'

import { WebStory } from '../../components/WebStory'

import { BrowserExtensionAlert } from './BrowserExtensionAlert'

const onAlertDismissed = action('onAlertDismissed')

const { add } = storiesOf('web/repo/actions/BrowserExtensionAlert', module)
add('standard', () => <WebStory>{() => <BrowserExtensionAlert onAlertDismissed={onAlertDismissed} />}</WebStory>)
