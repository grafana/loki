package assert

import "time"

// Assertions provides assertion methods around the
// TestingT interface.
type Assertions struct {
	t TestingT
}

// New makes a new Assertions object for the specified TestingT.
func New(t TestingT) *Assertions {
	return &Assertions{
		t: t,
	}
}

// Fail reports a failure through
func (a *Assertions) Fail(failureMessage string, msgAndArgs ...interface{}) {
	Fail(a.t, failureMessage, msgAndArgs...)
}

// Implements asserts that an object is implemented by the specified interface.
//
//    assert.Implements((*MyInterface)(nil), new(MyObject), "MyObject")
func (a *Assertions) Implements(interfaceObject interface{}, object interface{}, msgAndArgs ...interface{}) {
	Implements(a.t, interfaceObject, object, msgAndArgs...)
}

// IsType asserts that the specified objects are of the same type.
func (a *Assertions) IsType(expectedType interface{}, object interface{}, msgAndArgs ...interface{}) {
	IsType(a.t, expectedType, object, msgAndArgs...)
}

// Equal asserts that two objects are equal.
//
//    assert.Equal(123, 123, "123 and 123 should be equal")
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) Equal(expected, actual interface{}, msgAndArgs ...interface{}) {
	Equal(a.t, expected, actual, msgAndArgs...)
}

// EqualValues asserts that two objects are equal or convertable to the same types
// and equal.
//
//    assert.EqualValues(uint32(123), int32(123), "123 and 123 should be equal")
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) EqualValues(expected, actual interface{}, msgAndArgs ...interface{}) {
	EqualValues(a.t, expected, actual, msgAndArgs...)
}

// Exactly asserts that two objects are equal is value and type.
//
//    assert.Exactly(int32(123), int64(123), "123 and 123 should NOT be equal")
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) Exactly(expected, actual interface{}, msgAndArgs ...interface{}) {
	Exactly(a.t, expected, actual, msgAndArgs...)
}

// NotNil asserts that the specified object is not nil.
//
//    assert.NotNil(err, "err should be something")
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) NotNil(object interface{}, msgAndArgs ...interface{}) {
	NotNil(a.t, object, msgAndArgs...)
}

// Nil asserts that the specified object is nil.
//
//    assert.Nil(err, "err should be nothing")
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) Nil(object interface{}, msgAndArgs ...interface{}) {
	Nil(a.t, object, msgAndArgs...)
}

// Empty asserts that the specified object is empty.  I.e. nil, "", false, 0 or a
// slice with len == 0.
//
// assert.Empty(obj)
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) Empty(object interface{}, msgAndArgs ...interface{}) {
	Empty(a.t, object, msgAndArgs...)
}

// NotEmpty asserts that the specified object is NOT empty.  I.e. not nil, "", false, 0 or a
// slice with len == 0.
//
// if assert.NotEmpty(obj) {
//   assert.Equal("two", obj[1])
// }
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) NotEmpty(object interface{}, msgAndArgs ...interface{}) {
	NotEmpty(a.t, object, msgAndArgs...)
}

// Len asserts that the specified object has specific length.
// Len also fails if the object has a type that len() not accept.
//
//    assert.Len(mySlice, 3, "The size of slice is not 3")
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) Len(object interface{}, length int, msgAndArgs ...interface{}) {
	Len(a.t, object, length, msgAndArgs...)
}

// True asserts that the specified value is true.
//
//    assert.True(myBool, "myBool should be true")
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) True(value bool, msgAndArgs ...interface{}) {
	True(a.t, value, msgAndArgs...)
}

// False asserts that the specified value is true.
//
//    assert.False(myBool, "myBool should be false")
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) False(value bool, msgAndArgs ...interface{}) {
	False(a.t, value, msgAndArgs...)
}

// NotEqual asserts that the specified values are NOT equal.
//
//    assert.NotEqual(obj1, obj2, "two objects shouldn't be equal")
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) NotEqual(expected, actual interface{}, msgAndArgs ...interface{}) {
	NotEqual(a.t, expected, actual, msgAndArgs...)
}

// Contains asserts that the specified string contains the specified substring.
//
//    assert.Contains("Hello World", "World", "But 'Hello World' does contain 'World'")
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) Contains(s, contains interface{}, msgAndArgs ...interface{}) {
	Contains(a.t, s, contains, msgAndArgs...)
}

// NotContains asserts that the specified string does NOT contain the specified substring.
//
//    assert.NotContains("Hello World", "Earth", "But 'Hello World' does NOT contain 'Earth'")
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) NotContains(s, contains interface{}, msgAndArgs ...interface{}) {
	NotContains(a.t, s, contains, msgAndArgs...)
}

// Condition uses a Comparison to assert a complex condition.
func (a *Assertions) Condition(comp Comparison, msgAndArgs ...interface{}) {
	Condition(a.t, comp, msgAndArgs...)
}

// Panics asserts that the code inside the specified PanicTestFunc panics.
//
//   assert.Panics(func(){
//     GoCrazy()
//   }, "Calling GoCrazy() should panic")
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) Panics(f PanicTestFunc, msgAndArgs ...interface{}) {
	Panics(a.t, f, msgAndArgs...)
}

// NotPanics asserts that the code inside the specified PanicTestFunc does NOT panic.
//
//   assert.NotPanics(func(){
//     RemainCalm()
//   }, "Calling RemainCalm() should NOT panic")
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) NotPanics(f PanicTestFunc, msgAndArgs ...interface{}) {
	NotPanics(a.t, f, msgAndArgs...)
}

// WithinDuration asserts that the two times are within duration delta of each other.
//
//   assert.WithinDuration(time.Now(), time.Now(), 10*time.Second, "The difference should not be more than 10s")
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) WithinDuration(expected, actual time.Time, delta time.Duration, msgAndArgs ...interface{}) {
	WithinDuration(a.t, expected, actual, delta, msgAndArgs...)
}

// InDelta asserts that the two numerals are within delta of each other.
//
// 	 assert.InDelta(t, math.Pi, (22 / 7.0), 0.01)
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) InDelta(expected, actual interface{}, delta float64, msgAndArgs ...interface{}) {
	InDelta(a.t, expected, actual, delta, msgAndArgs...)
}

// InEpsilon asserts that expected and actual have a relative error less than epsilon
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) InEpsilon(expected, actual interface{}, epsilon float64, msgAndArgs ...interface{}) {
	InEpsilon(a.t, expected, actual, epsilon, msgAndArgs...)
}

// NoError asserts that a function returned no error (i.e. `nil`).
//
//   actualObj, err := SomeFunction()
//   if assert.NoError(err) {
//	   assert.Equal(actualObj, expectedObj)
//   }
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) NoError(theError error, msgAndArgs ...interface{}) {
	NoError(a.t, theError, msgAndArgs...)
}

// Error asserts that a function returned an error (i.e. not `nil`).
//
//   actualObj, err := SomeFunction()
//   if assert.Error(err, "An error was expected") {
//	   assert.Equal(err, expectedError)
//   }
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) Error(theError error, msgAndArgs ...interface{}) {
	Error(a.t, theError, msgAndArgs...)
}

// EqualError asserts that a function returned an error (i.e. not `nil`)
// and that it is equal to the provided error.
//
//   actualObj, err := SomeFunction()
//   if assert.Error(err, "An error was expected") {
//	   assert.Equal(err, expectedError)
//   }
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) EqualError(theError error, errString string, msgAndArgs ...interface{}) {
	EqualError(a.t, theError, errString, msgAndArgs...)
}

// Regexp asserts that a specified regexp matches a string.
//
//  assert.Regexp(t, regexp.MustCompile("start"), "it's starting")
//  assert.Regexp(t, "start...$", "it's not starting")
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) Regexp(rx interface{}, str interface{}, msgAndArgs ...interface{}) {
	Regexp(a.t, rx, str, msgAndArgs...)
}

// NotRegexp asserts that a specified regexp does not match a string.
//
//  assert.NotRegexp(t, regexp.MustCompile("starts"), "it's starting")
//  assert.NotRegexp(t, "^start", "it's not starting")
//
// Returns whether the assertion was successful (true) or not (false).
func (a *Assertions) NotRegexp(rx interface{}, str interface{}, msgAndArgs ...interface{}) {
	NotRegexp(a.t, rx, str, msgAndArgs...)
}

// Zero asserts that i is the zero value for its type and returns the truth.
func (a *Assertions) Zero(i interface{}, msgAndArgs ...interface{}) {
	Zero(a.t, i, msgAndArgs...)
}

// NotZero asserts that i is not the zero value for its type and returns the truth.
func (a *Assertions) NotZero(i interface{}, msgAndArgs ...interface{}) {
	NotZero(a.t, i, msgAndArgs...)
}
