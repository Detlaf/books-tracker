import { describe, it, expect } from 'vitest'
import { parseDelimitedText, toRecord } from './import'
import { normalizeStatus } from './status'

describe('parseDelimitedText', () => {
  it('parses comma-separated rows into header-keyed objects', () => {
    const rows = parseDelimitedText('Title,Author\nCirce,Madeline Miller')
    expect(rows).toEqual([{ title: 'Circe', author: 'Madeline Miller' }])
  })

  it('switches to tabs when the header row has one', () => {
    const rows = parseDelimitedText('Title\tAuthor\nCirce\tMadeline Miller')
    expect(rows).toEqual([{ title: 'Circe', author: 'Madeline Miller' }])
  })

  it('skips blank lines and tolerates short rows', () => {
    const rows = parseDelimitedText('Title,Author\nCirce\n\n')
    expect(rows).toEqual([{ title: 'Circe', author: '' }])
  })

  it('returns nothing for an empty file', () => {
    expect(parseDelimitedText('')).toEqual([])
  })
})

describe('toRecord', () => {
  it('maps a full row', () => {
    const record = toRecord({
      title: 'Piranesi',
      author: 'Susanna Clarke',
      status: 'Read',
      rating: '5',
      'date finished': '2024-11-20',
      language: 'English',
      isbn: '978-0-13-556789-2',
    })
    expect(record).toEqual({
      title: 'Piranesi',
      author: 'Susanna Clarke',
      status: 'read',
      rating: 5,
      finishedAt: '2024-11-20',
      language: 'English',
      isbn: '9780135567892',
    })
  })

  it('drops a row with no title', () => {
    expect(toRecord({ author: 'Nobody' })).toBeNull()
  })

  it('defaults a missing author and status', () => {
    const record = toRecord({ title: 'Untitled' })
    expect(record.author).toBe('Unknown')
    expect(record.status).toBe('backlog')
    expect(record.rating).toBe(0)
    expect(record.finishedAt).toBeNull()
  })

  it('clamps an out-of-range rating', () => {
    expect(toRecord({ title: 'X', rating: '9' }).rating).toBe(5)
    expect(toRecord({ title: 'X', rating: '-3' }).rating).toBe(0)
    expect(toRecord({ title: 'X', rating: 'abc' }).rating).toBe(0)
  })

  it('accepts the alternate header spellings', () => {
    expect(toRecord({ 'book title': 'Circe', authors: 'Miller' })).toMatchObject({
      title: 'Circe',
      author: 'Miller',
    })
    expect(toRecord({ title: 'X', datefinished: '2020-01-01' }).finishedAt).toBe('2020-01-01')
  })
})

describe('normalizeStatus', () => {
  it('maps the design and export spellings onto the API vocabulary', () => {
    expect(normalizeStatus('read')).toBe('read')
    expect(normalizeStatus('Finished')).toBe('read')
    expect(normalizeStatus('in progress')).toBe('reading')
    expect(normalizeStatus('to-read')).toBe('backlog')
    expect(normalizeStatus('want to read')).toBe('backlog')
  })

  it('falls back to the backlog for anything unrecognized', () => {
    // Better to surface the book for correction than to drop the row.
    expect(normalizeStatus('')).toBe('backlog')
    expect(normalizeStatus('???')).toBe('backlog')
  })
})
