# Sinatra

[![Build Status](https://secure.travis-ci.org/sinatra/sinatra.svg)](http://travis-ci.org/sinatra/sinatra)

Sinatra es un
[DSL](https://es.wikipedia.org/wiki/Lenguaje_específico_del_dominio) para
crear aplicaciones web rápidamente en Ruby con un mínimo esfuerzo:

```ruby
# miapp.rb
require 'sinatra'

get '/' do
  'Hola mundo!'
end
```

Instala la gema:

```shell
gem install sinatra
ruby miapp.rb
```

Y corre la aplicación:
```shell
gem install sinatra
ruby miapp.rb
```

Ver en [http://localhost:4567](http://localhost:4567).

El código que cambiaste no tendra efecto hasta que reinicies el servidor.
Por favor reinicia el servidor cada vez que cambies tu código o usa [sinatra/reloader](http://www.sinatrarb.com/contrib/reloader).

Se recomienda ejecutar `gem install thin`, porque Sinatra lo utilizará si está disponible.


## Tabla de Contenidos

* [Sinatra](#sinatra)
    * [Tabla de Contenidos](#tabla-de-contenidos)
    * [Rutas](#rutas)
    * [Condiciones](#condiciones)
    * [Valores de Retorno](#valores-de-retorno)
    * [Comparadores de Rutas Personalizados](#comparadores-de-rutas-personalizados)
    * [Archivos Estáticos](#archivos-estáticos)
    * [Vistas / Plantillas](#vistas--plantillas)
        * [Plantillas Literales](#plantillas-literales)
        * [Lenguajes de Plantillas Disponibles](#lenguajes-de-plantillas-disponibles)
            * [Plantillas Haml](#plantillas-haml)
            * [Plantillas Erb](#plantillas-erb)
            * [Plantillas Builder](#plantillas-builder)
            * [Plantillas Nokogiri](#plantillas-nokogiri)
            * [Plantillas Sass](#plantillas-sass)
            * [Plantillas SCSS](#plantillas-scss)
            * [Plantillas Less](#plantillas-less)
            * [Plantillas Liquid](#plantillas-liquid)
            * [Plantillas Markdown](#plantillas-markdown)
            * [Plantillas Textile](#plantillas-textile)
            * [Plantillas RDoc](#plantillas-rdoc)
            * [Plantillas AsciiDoc](#plantillas-asciidoc)
            * [Plantillas Radius](#plantillas-radius)
            * [Plantillas Markaby](#plantillas-markaby)
            * [Plantillas RABL](#plantillas-rabl)
            * [Plantillas Slim](#plantillas-slim)
            * [Plantillas Creole](#plantillas-creole)
            * [Plantillas MediaWiki](#mediawiki-templates)
            * [Plantillas CofeeScript](#plantillas-coffeescript)
            * [Plantillas Stylus](#plantillas-stylus)
            * [Plantillas Yajl](#plantillas-yajl)
            * [Plantillas Wlang](#plantillas-wlang)
        * [Accediendo Variables en Plantillas](#accediendo-a-variables-en-plantillas)
        * [Plantillas con `yield` y `layout` anidado](#plantillas-con-yield-y-layout-anidado)
        * [Plantillas Inline](#plantillas-inline)
        * [Plantillas Nombradas](#plantillas-nombradas)
        * [Asociando Extensiones de Archivo](#asociando-extensiones-de-archivo)
        * [Añadiendo Tu Propio Motor de Plantillas](#añadiendo-tu-propio-motor-de-plantillas)
        * [Usando Lógica Personalizada para la Búsqueda en Plantillas](#usando-lógica-personalizada-para-la-búsqueda-en-plantillas)
    * [Filtros](#filtros)
    * [Helpers](#helpers)
        * [Usando Sesiones](#usando-sesiones)
          * [Secreto de Sesión](#secreto-de-sesión)
          * [Configuración de Sesión](#configuración-de-sesión)
          * [Escogiendo tu propio Middleware de Sesión](#escogiendo-tu-propio-middleware-de-sesión)
        * [Interrupcion](#interrupción)
        * [Paso](#paso)
        * [Desencadenando Otra Ruta](#desencadenando-otra-ruta)
        * [Configurando el Cuerpo, Código de Estado y los Encabezados](#configurando-el-cuerpo-código-de-estado-y-los-encabezados)
        * [Streaming De Respuestas](#streaming-de-respuestas)
        * [Logging](#logging)
        * [Tipos Mime](#tipos-mime)
        * [Generando URLs](#generando-urls)
        * [Redirección del Navegador](#redirección-del-navegador)
        * [Control del Cache](#control-del-cache)
        * [Enviando Archivos](#enviando-archivos)
        * [Accediendo al Objeto Request](#accediendo-al-objeto-request)
        * [Archivos Adjuntos](#archivos-adjuntos)
        * [Fecha y Hora](#fecha-y-hora)
        * [Buscando los Archivos de las Plantillas](#buscando-los-archivos-de-las-plantillas)
    * [Configuración](#configuración)
        * [Configurando la Protección Contra Ataques](#configurando-la-protección-contra-ataques)
        * [Configuraciones Disponibles](#configuraciones-disponibles)
    * [Entornos](#entornos)
    * [Manejo de Errores](#manejo-de-errores)
        * [Not Found](#not-found)
        * [Error](#error)
    * [Rack Middleware](#rack-middleware)
    * [Pruebas](#pruebas)
    * [Sinatra::Base - Middleware, Librerías, y Aplicaciones Modulares](#sinatrabase---middleware-librerías-y-aplicaciones-modulares)
        * [Estilo Modular vs Estilo Clásico](#estilo-modular-vs-clásico)
        * [Sirviendo una Aplicación Modular](#sirviendo-una-aplicación-modular)
        * [Usando una Aplicación de Estilo Clásico con config.ru](#usando-una-aplicación-clásica-con-un-archivo-configru)
        * [¿Cuándo usar config.ru?](#cuándo-usar-configru)
        * [Utilizando Sinatra como Middleware](#utilizando-sinatra-como-middleware)
        * [Creación Dinámica de Aplicaciones](#creación-dinámica-de-aplicaciones)
    * [Ámbitos y Ligaduras (Scopes and Binding)](#Ámbitos-y-ligaduras)
        * [Alcance de una Aplicación/Clase](#Ámbito-de-aplicaciónclase)
        * [Alcance de una Solicitud/Instancia](#Ámbito-de-peticióninstancia)
        * [Alcance de Delegación](#Ámbito-de-delegación)
    * [Línea de comandos](#línea-de-comandos)
        * [Multi-threading](#multi-threading)
    * [Requerimientos](#requerimientos)
    * [A la Vanguardia](#a-la-vanguardia)
        * [Usando bundler](#usando-bundler)
    * [Versionado](#versionado)
    * [Lecturas Recomendadas](#lecturas-recomendadas)

## Rutas

En Sinatra, una ruta es un método HTTP junto a un patrón de un URL.
Cada ruta está asociada a un bloque:

```ruby
get '/' do
  .. mostrar algo ..
end

post '/' do
  .. crear algo ..
end

put '/' do
  .. reemplazar algo ..
end

patch '/' do
  .. modificar algo ..
end

delete '/' do
  .. aniquilar algo ..
end

options '/' do
  .. informar algo ..
end

link '/' do
  .. afiliar a algo ..
end

unlink '/' do
  .. separar algo ..
end

```

Las rutas son comparadas en el orden en el que son definidas. La primera ruta
que coincide con la petición es invocada.

Las rutas con barras al final son distintas a las que no tienen:


```ruby
get '/foo' do
  # no es igual que "GET /foo/"
end
```

Los patrones de las rutas pueden incluir parámetros nombrados, accesibles a
través del hash `params`:


```ruby
get '/hola/:nombre' do
  # coincide con "GET /hola/foo" y "GET /hola/bar"
  # params['nombre'] es 'foo' o 'bar'
  "Hola #{params['nombre']}!"
end
```

También puede acceder a los parámetros nombrados usando parámetros de bloque:

```ruby
get '/hola/:nombre' do |n|
  # coincide con "GET /hola/foo" y "GET /hola/bar"
  # params['nombre'] es 'foo' o 'bar'
  # n almacena params['nombre']
  "Hola #{n}!"
end
```

Los patrones de ruta también pueden incluir parámetros splat (o wildcard),
accesibles a través del arreglo `params['splat']`:

```ruby
get '/decir/*/al/*' do
  # coincide con /decir/hola/al/mundo
  params['splat'] # => ["hola", "mundo"]
end

get '/descargar/*.*' do
  # coincide con /descargar/path/al/archivo.xml
  params['splat'] # => ["path/al/archivo", "xml"]
end
```

O, con parámetros de bloque:

```ruby
get '/descargar/*.*' do |path, ext|
  [path, ext] # => ["path/to/file", "xml"]
end
```

Rutas con Expresiones Regulares:

```ruby
get /\/hola\/([\w]+)/ do
  "Hola, #{params['captures'].first}!"
end
```

O con un parámetro de bloque:

```ruby
get %r{/hola/([\w]+)} do |c|
  "Hola, #{c}!"
end
```

Los patrones de ruta pueden contener parámetros opcionales:

```ruby
get '/posts/:formato?' do
  # coincide con "GET /posts/" y además admite cualquier extensión, por
  # ejemplo, "GET /posts/json", "GET /posts/xml", etc.
end
```

Las rutas también pueden usar parámetros de consulta:

```ruby
get '/posts' do
  # es igual que "GET /posts?title=foo&author=bar"
  title = params['title']
  author = params['author']
  # usa las variables title y author; la consulta es opcional para la ruta /posts
end
```

A propósito, a menos que desactives la protección para el ataque *path
traversal* (ver más [abajo](#configurando-la-protección-contra-ataques)), el path de la petición puede ser modificado
antes de que se compare con los de tus rutas.

Puedes perzonalizar las opciones de [Mustermann](https://github.com/sinatra/mustermann) usadas para una ruta pasando
el hash `:mustermann_opts`:

```ruby
get '\A/posts\z', :mustermann_opts => { :type => :regexp, :check_anchors => false } do
  # es exactamente igual a /posts, con anclaje explícito
  "¡Si igualas un patrón anclado aplaude!"
end
```

## Condiciones

Las rutas pueden incluir una variedad de condiciones de selección, como por
ejemplo el user agent:

```ruby
get '/foo', :agent => /Songbird (\d\.\d)[\d\/]*?/ do
  "Estás usando la versión de Songbird #{params['agent'][0]}"
end

get '/foo' do
  # Coincide con navegadores que no sean songbird
end
```

Otras condiciones disponibles son `host_name` y `provides`:

```ruby
get '/', :host_name => /^admin\./ do
  "Área de Administración, Acceso denegado!"
end

get '/', :provides => 'html' do
  haml :index
end

get '/', :provides => ['rss', 'atom', 'xml'] do
  builder :feed
end
```

`provides` busca el encabezado Accept de la solicitud


Puede definir sus propias condiciones fácilmente:

```ruby
set(:probabilidad) { |valor| condition { rand <= valor } }

get '/gana_un_auto', :probabilidad => 0.1 do
  "Ganaste!"
end

get '/gana_un_auto' do
  "Lo siento, perdiste."
end
```

Para una condición que toma multiples valores usa splat:

```ruby
set(:autorizar) do |*roles|   # <- mira el splat
  condition do
    unless sesion_iniciada? && roles.any? {|rol| usuario_actual.tiene_rol? rol }
      redirect "/iniciar_sesion/", 303
    end
  end
end

get "/mi/cuenta/", :autorizar => [:usuario, :administrador] do
  "Detalles de mi cuenta"
end

get "/solo/administradores/", :autorizar => :administrador do
  "Únicamente para administradores!"
end
```

## Valores de Retorno

El valor de retorno de un bloque de ruta determina por lo menos el cuerpo de la respuesta
transmitida al cliente HTTP o por lo menos al siguiente middleware en la pila de Rack.
Lo más común es que sea un string, como en los ejemplos anteriores.
Sin embargo, otros valores también son aceptados.

Puedes retornar cualquier objeto que sea una respuesta Rack válida,
un objeto que represente el cuerpo de una respuesta Rack o un código
de estado HTTP:

* Un arreglo con tres elementos: `[estado (Integer), cabeceras (Hash), cuerpo de
  la respuesta (responde a #each)]`
* Un arreglo con dos elementos: `[estado (Integer), cuerpo de la respuesta
  (responde a #each)]`
* Un objeto que responde a `#each` y que le pasa únicamente strings al bloque
  dado
* Un Integer representando el código de estado

De esa manera, por ejemplo, podemos fácilmente implementar un ejemplo de streaming:

```ruby
class Stream
  def each
    100.times { |i| yield "#{i}\n" }
  end
end

get('/') { Stream.new }
```

También puedes usar el `stream` helper ([descrito abajo](#streaming-de-respuestas))
para reducir el código repetitivo e incrustar la lógica de stream a la ruta

## Comparadores de Rutas Personalizados

Como se mostró anteriormente, Sinatra permite utilizar strings y expresiones
regulares para definir las rutas. Sin embargo, no termina ahí.Puedes
definir tus propios comparadores muy fácilmente:

```ruby
class TodoMenosElPatron
  Match = Struct.new(:captures)

  def initialize(excepto)
    @excepto  = excepto
    @capturas = Match.new([])
  end

  def match(str)
    @capturas unless @excepto === str
  end
end

def cualquiera_menos(patron)
  TodoMenosElPatron.new(patron)
end

get cualquiera_menos("/index") do
  # ...
end
```

Tenga en cuenta que el ejemplo anterior es un poco rebuscado. Un resultado
similar puede conseguirse más sencillamente:

```ruby
get // do
  pass if request.path_info == "/index"
  # ...
end
```

O, usando un look ahead negativo:

```ruby
get %r{(?!/index)} do
  # ...
end
```

## Archivos Estáticos

Los archivos estáticos son servidos desde el directorio público
`./public`. Puede especificar una ubicación diferente ajustando la
opción `:public_folder`:

```ruby
set :public_folder, File.dirname(__FILE__) + '/static'
```

Note que el nombre del directorio público no está incluido en la URL. Por
ejemplo, el archivo `./public/css/style.css` se accede a través de
`http://ejemplo.com/css/style.css`.

Use la configuración `:static_cache_control` para agregar el encabezado
`Cache-Control` (Ver mas [abajo](#control-del-cache)).

## Vistas / Plantillas

Cada lenguaje de plantilla se expone a través de un método de renderizado que
lleva su nombre. Estos métodos simplemente devuelven un string:

```ruby
get '/' do
  erb :index
end
```

Renderiza `views/index.erb`.

En lugar del nombre de la plantilla puedes proporcionar directamente el
contenido de la misma:

```ruby
get '/' do
  codigo = "<%= Time.now %>"
  erb codigo
end
```

Los métodos de renderizado, aceptan además un segundo argumento, el hash de
opciones:

```ruby
get '/' do
  erb :index, :layout => :post
end
```

Renderiza `views/index.erb` incrustado en `views/post.erb` (por
defecto, la plantilla `:index` es incrustada en `views/layout.erb` siempre y
cuando este último archivo exista).

Cualquier opción que Sinatra no entienda le será pasada al motor de renderizado
de la plantilla:

```ruby
get '/' do
  haml :index, :format => :html5
end
```

Además, puede definir las opciones para un lenguaje de plantillas de forma
general:

```ruby
set :haml, :format => :html5

get '/' do
  haml :index
end
```

Las opciones pasadas al método de renderizado tienen precedencia sobre las
definidas mediante `set`.

Opciones disponibles:

<dl>

  <dt>locals</dt>
  <dd>
    Lista de variables locales pasadas al documento. Resultan muy útiles cuando
    se combinan con parciales.
    Ejemplo: <tt>erb "<%= foo %>", :locals => {:foo => "bar"}</tt>
  </dd>

  <dt>default_encoding</dt>
  <dd>
    Encoding utilizado cuando el de un string es dudoso. Por defecto toma el
    valor de <tt>settings.default_encoding</tt>.
  </dd>

  <dt>views</dt>
  <dd>
    Directorio desde donde se cargan las vistas. Por defecto toma el valor de
    <tt>settings.views</tt>.
  </dd>

  <dt>layout</dt>
  <dd>
    Si es <tt>true</tt> o <tt>false</tt> indica que se debe usar, o no, un layout,
    respectivamente. También puede ser un Symbol que especifique qué plantilla
    usar. Ejemplo: <tt>erb :index, :layout => !request.xhr?</tt>
  </dd>

  <dt>content_type</dt>
  <dd>
    Content-Type que produce la plantilla. El valor por defecto depende de cada
    lenguaje de plantillas.
  </dd>

  <dt>scope</dt>
  <dd>
    Ámbito en el que se renderiza la plantilla. Por defecto utiliza la instancia
    de la aplicación. Ten en cuenta que si cambiás esta opción las variables de
    instancia y los helpers van a dejar de estar disponibles.
  </dd>

  <dt>layout_engine</dt>
  <dd>
    Motor de renderizado de plantillas que usa para el layout. Resulta
    conveniente para lenguajes que no soportan layouts. Por defecto toma el valor
    del motor usado para renderizar la plantilla.
    Ejemplo: <tt>set :rdoc, :layout_engine => :erb</tt>
  </dd>
  
  <dt>layout_options</dt>
  <dd>
    Opciones especiales usadas únicamente para renderizar el layout. Ejemplo:
    <tt>set :rdoc, :layout_options => { :views => 'views/layouts' }</tt>
  </dd>
</dl>

Se asume que las plantillas están ubicadas directamente bajo el directorio `./views`.
Para usar un directorio diferente:

```ruby
set :views, settings.root + '/templates'
```

Es importante acordarse que siempre tienes que referenciar a las plantillas con
símbolos, incluso cuando se encuentran en un subdirectorio (en este caso
tienes que usar: `:'subdir/plantilla'` o `'subdir/plantilla'.to_sym`). Esto es debido
a que los métodos de renderización van a renderizar directamente cualquier string que
se les pase como argumento.

### Plantillas Literales

```ruby
get '/' do
  haml '%div.titulo Hola Mundo'
end
```

Renderiza el string de la plantilla. Opcionalmente puedes especificar 
`:path` y `:line` para un backtrace más claro si hay una ruta del sistema
de archivos o una línea asociada con ese string

```ruby
get '/' do
  haml '%div.titulo Hola Mundo', :path => 'ejemplos/archivo.haml', :line => 3
end
```

### Lenguajes de Plantillas Disponibles

Algunos lenguajes tienen varias implementaciones. Para especificar que
implementación usar (y para ser thread-safe), deberías requerirla antes de
usarla:

```ruby
require 'rdiscount' # o require 'bluecloth'
get('/') { markdown :index }
```

#### Plantillas Haml

<table>
  <tr>
    <td>Dependencias</td>
    <td><a href="http://haml.info/" title="haml">haml</a></td>
  </tr>
  <tr>
    <td>Expresiones de Archivo</td>
    <td><tt>.haml</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>haml :index, :format => :html5</tt></td>
  </tr>
</table>

#### Plantillas Erb

<table>
  <tr>
    <td>Dependencias</td>
    <td>
      <a href="http://www.kuwata-lab.com/erubis/" title="erubis">erubis</a>
      o erb (incluida en Ruby)
    </td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.erb</tt>, <tt>.rhtml</tt> o <tt>.erubis</tt> (solamente con Erubis)</td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>erb :index</tt></td>
  </tr>
</table>

#### Plantillas Builder

<table>
  <tr>
    <td>Dependencias</td>
    <td>
      <a href="https://github.com/jimweirich/builder" title="builder">builder</a>
    </td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.builder</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>builder { |xml| xml.em "hola" }</tt></td>
  </tr>
</table>

También toma un bloque para plantillas inline (ver [ejemplo](#plantillas-inline)).

#### Plantillas Nokogiri

<table>
  <tr>
    <td>Dependencias</td>
    <td><a href="http://www.nokogiri.org/" title="nokogiri">nokogiri</a></td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.nokogiri</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>nokogiri { |xml| xml.em "hola" }</tt></td>
  </tr>
</table>

También toma un bloque para plantillas inline (ver [ejemplo](#plantillas-inline)).

#### Plantillas Sass

<table>
  <tr>
    <td>Dependencias</td>
    <td><a href="http://sass-lang.com/" title="sass">sass</a></td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.sass</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>sass :stylesheet, :style => :expanded</tt></td>
  </tr>
</table>

#### Plantillas SCSS

<table>
  <tr>
    <td>Dependencias</td>
    <td><a href="http://sass-lang.com/" title="sass">sass</a></td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.scss</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>scss :stylesheet, :style => :expanded</tt></td>
  </tr>
</table>

#### Plantillas Less

<table>
  <tr>
    <td>Dependencias</td>
    <td><a href="http://lesscss.org/" title="less">less</a></td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.less</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>less :stylesheet</tt></td>
  </tr>
</table>

#### Plantillas Liquid

<table>
  <tr>
    <td>Dependencias</td>
    <td><a href="https://shopify.github.io/liquid/" title="liquid">liquid</a></td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.liquid</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>liquid :index, :locals => { :clave => 'valor' }</tt></td>
  </tr>
</table>

Como no va a poder llamar a métodos de Ruby (excepto por `yield`) desde una
plantilla Liquid, casi siempre va a querer pasarle locales.

#### Plantillas Markdown

<table>
  <tr>
    <td>Dependencias</td>
    <td>
      <a href="https://github.com/davidfstr/rdiscount" title="RDiscount">RDiscount</a>,
      <a href="https://github.com/vmg/redcarpet" title="RedCarpet">RedCarpet</a>,
      <a href="https://github.com/ged/bluecloth" title="bluecloth">BlueCloth</a>,
      <a href="http://kramdown.gettalong.org/" title="kramdown">kramdown</a> o
      <a href="https://github.com/bhollis/maruku" title="maruku">maruku</a>
    </td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.markdown</tt>, <tt>.mkd</tt> y <tt>.md</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>markdown :index, :layout_engine => :erb</tt></td>
  </tr>
</table>

No es posible llamar métodos desde markdown, ni pasarle locales. Por lo tanto,
generalmente va a usarlo en combinación con otro motor de renderizado:

```ruby
erb :resumen, :locals => { :texto => markdown(:introduccion) }
```

Tenga en cuenta que también puedes llamar al método `markdown` desde otras
plantillas:

```ruby
%h1 Hola Desde Haml!
%p= markdown(:saludos)
```

Como no puede utilizar Ruby desde Markdown, no puede usar layouts escritos en
Markdown. De todos modos, es posible usar un motor de renderizado para el
layout distinto al de la plantilla pasando la opción `:layout_engine`.

#### Plantillas Textile

<table>
  <tr>
    <td>Dependencias</td>
    <td><a href="http://redcloth.org/" title="RedCloth">RedCloth</a></td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.textile</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>textile :index, :layout_engine => :erb</tt></td>
  </tr>
</table>

No es posible llamar métodos desde textile, ni pasarle locales. Por lo tanto,
generalmente vas a usarlo en combinación con otro motor de renderizado:

```ruby
erb :resumen, :locals => { :texto => textile(:introduccion) }
```

Ten en cuenta que también puedes llamar al método `textile` desde otras
plantillas:

```ruby
%h1 Hola Desde Haml!
%p= textile(:saludos)
```

Como no puedes utilizar Ruby desde Textile, no puedes usar layouts escritos en
Textile. De todos modos, es posible usar un motor de renderizado para el
layout distinto al de la plantilla pasando la opción `:layout_engine`.

#### Plantillas RDoc

<table>
  <tr>
    <td>Dependencias</td>
    <td><a href="http://rdoc.sourceforge.net/" title="RDoc">RDoc</a></td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.rdoc</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>rdoc :README, :layout_engine => :erb</tt></td>
  </tr>
</table>

No es posible llamar métodos desde rdoc, ni pasarle locales. Por lo tanto,
generalmente vas a usarlo en combinación con otro motor de renderizado:

```ruby
erb :resumen, :locals => { :texto => rdoc(:introduccion) }
```

Ten en cuenta que también puedes llamar al método `rdoc` desde otras
plantillas:

```ruby
%h1 Hola Desde Haml!
%p= rdoc(:saludos)
```

Como no puedes utilizar Ruby desde RDoc, no puedes usar layouts escritos en RDoc.
De todos modos, es posible usar un motor de renderizado para el layout distinto
al de la plantilla pasando la opción `:layout_engine`.

#### Plantillas AsciiDoc

<table>
  <tr>
    <td>Dependencia</td>
    <td><a href="http://asciidoctor.org/" title="Asciidoctor">Asciidoctor</a></td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.asciidoc</tt>, <tt>.adoc</tt> and <tt>.ad</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>asciidoc :README, :layout_engine => :erb</tt></td>
  </tr>
</table>

Desde que no se puede utilizar métodos de Ruby desde una
plantilla AsciiDoc, casi siempre va a querer pasarle locales.

#### Plantillas Radius

<table>
  <tr>
    <td>Dependencias</td>
    <td><a href="https://github.com/jlong/radius" title="Radius">Radius</a></td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.radius</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>radius :index, :locals => { :clave => 'valor' }</tt></td>
  </tr>
</table>

Desde que no se puede utilizar métodos de Ruby (excepto por `yield`) de una
plantilla Radius, casi siempre se necesita pasar locales.

#### Plantillas Markaby

<table>
  <tr>
    <td>Dependencias</td>
    <td><a href="http://markaby.github.io/" title="Markaby">Markaby</a></td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.mab</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>markaby { h1 "Bienvenido!" }</tt></td>
  </tr>
</table>

También toma un bloque para plantillas inline (ver [ejemplo](#plantillas-inline)).

#### Plantillas RABL

<table>
  <tr>
    <td>Dependencias</td>
    <td><a href="https://github.com/nesquena/rabl" title="Rabl">Rabl</a></td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.rabl</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>rabl :index</tt></td>
  </tr>
</table>

#### Plantillas Slim

<table>
  <tr>
    <td>Dependencias</td>
    <td><a href="http://slim-lang.com/" title="Slim Lang">Slim Lang</a></td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.slim</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>slim :index</tt></td>
  </tr>
</table>

#### Plantillas Creole

<table>
  <tr>
    <td>Dependencias</td>
    <td><a href="https://github.com/minad/creole" title="Creole">Creole</a></td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.creole</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>creole :wiki, :layout_engine => :erb</tt></td>
  </tr>
</table>

No es posible llamar métodos desde creole, ni pasarle locales. Por lo tanto,
generalmente va a usarlo en combinación con otro motor de renderizado:

```ruby
erb :resumen, :locals => { :texto => cerole(:introduccion) }
```

Debe tomar en cuenta que también puede llamar al método `creole` desde otras
plantillas:

```ruby
%h1 Hola Desde Haml!
%p= creole(:saludos)
```

Como no puedes utilizar Ruby desde Creole, no puedes usar layouts escritos en
Creole. De todos modos, es posible usar un motor de renderizado para el layout
distinto al de la plantilla pasando la opción `:layout_engine`.

#### MediaWiki Templates

<table>
  <tr>
    <td>Dependencia</td>
    <td><a href="https://github.com/nricciar/wikicloth" title="WikiCloth">WikiCloth</a></td>
  </tr>
  <tr>
    <td>Extension de Archivo</td>
    <td><tt>.mediawiki</tt> and <tt>.mw</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>mediawiki :wiki, :layout_engine => :erb</tt></td>
  </tr>
</table>

No es posible llamar métodos desde el markup de MediaWiki, ni pasar locales al mismo.
Por lo tanto usualmente lo usarás en combinación con otro motor de renderizado:

```ruby
erb :overview, :locals => { :text => mediawiki(:introduction) }
```

Nota que también puedes llamar al método `mediawiki` desde dentro de otras plantillas:

```ruby
%h1 Hello From Haml!
%p= mediawiki(:greetings)
```

Debido a que no puedes llamar a Ruby desde MediaWiki, no puedes usar los diseños escritos en MediaWiki.
De todas maneras, es posible usar otro motor de renderizado para esa plantilla pasando la opción :layout_engine.

#### Plantillas CoffeeScript

<table>
  <tr>
    <td>Dependencias</td>
    <td>
      <a href="https://github.com/josh/ruby-coffee-script" title="Ruby CoffeeScript">
        CoffeeScript
      </a> y un
      <a href="https://github.com/sstephenson/execjs/blob/master/README.md#readme" title="ExecJS">
        mecanismo para ejecutar javascript
      </a>
    </td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.coffee</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>coffee :index</tt></td>
  </tr>
</table>

#### Plantillas Stylus

<table>
  <tr>
    <td>Dependencias</td>
    <td>
      <a href="https://github.com/forgecrafted/ruby-stylus" title="Ruby Stylus">
        Stylus
      </a> y un
      <a href="https://github.com/sstephenson/execjs/blob/master/README.md#readme" title="ExecJS">
        mecanismo para ejecutar javascript
      </a>
    </td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.styl</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>stylus :index</tt></td>
  </tr>
</table>

Antes de poder usar las plantillas de Stylus, necesitas cargar `stylus` y `stylus/tilt`:

```ruby
require 'sinatra'
require 'stylus'
require 'stylus/tilt'

get '/' do
  stylus :example
end
```

#### Plantillas Yajl

<table>
  <tr>
    <td>Dependencias</td>
    <td><a href="https://github.com/brianmario/yajl-ruby" title="yajl-ruby">yajl-ruby</a></td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.yajl</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td>
      <tt>
        yajl :index,
             :locals => { :key => 'qux' },
             :callback => 'present',
             :variable => 'resource'
      </tt>
    </td>
  </tr>
</table>

El contenido de la plantilla se evalúa como código Ruby, y la variable `json` es convertida a JSON mediante `#to_json`.

```ruby
json = { :foo => 'bar' }
json[:baz] = key
```

Las opciones `:callback` y `:variable` se pueden utilizar para decorar el objeto renderizado:

```ruby
var contenido = {"foo":"bar","baz":"qux"};
present(contenido);
```

#### Plantillas WLang

<table>
  <tr>
    <td>Dependencias</td>
    <td><a href="https://github.com/blambeau/wlang/" title="wlang">wlang</a></td>
  </tr>
  <tr>
    <td>Extensiones de Archivo</td>
    <td><tt>.wlang</tt></td>
  </tr>
  <tr>
    <td>Ejemplo</td>
    <td><tt>wlang :index, :locals => { :clave => 'valor' }</tt></td>
  </tr>
</table>

Como no vas a poder llamar a métodos de Ruby (excepto por `yield`) desde una
plantilla WLang, casi siempre vas a querer pasarle locales.

### Accediendo a Variables en Plantillas

Las plantillas son evaluadas dentro del mismo contexto que los manejadores de
ruta. Las variables de instancia asignadas en los manejadores de ruta son
accesibles directamente por las plantillas:

```ruby
get '/:id' do
  @foo = Foo.find(params['id'])
  haml '%h1= @foo.nombre'
end
```

O es posible especificar un Hash de variables locales explícitamente:

```ruby
get '/:id' do
  foo = Foo.find(params['id'])
  haml '%h1= bar.nombre', :locals => { :bar => foo }
end
```

Esto es usado típicamente cuando se renderizan plantillas como parciales desde
adentro de otras plantillas.

### Plantillas con `yield` y `layout` anidado

Un layout es usualmente una plantilla que llama a `yield`.
Dicha plantilla puede ser usada tanto a travé de la opción `:template`
como describimos arriba, o puede ser rederizada con un bloque como a
continuación:

```ruby
erb :post, :layout => false do
  erb :index
end
```
Este código es principalmente equivalente a `erb :index, :layout => :post`.

Pasar bloques a métodos de renderizado es la forma mas útil de crear layouts anidados:

```ruby
erb :main_layout, :layout => false do
  erb :admin_layout do
    erb :user
  end
end
```

Esto también se puede hacer en menos líneas de código con:

```ruby
erb :admin_layout, :layout => :main_layout do
  erb :user
end
```

Actualmente, los siguientes métodos de renderizado aceptan un bloque: `erb`, `haml`,
`liquid`, `slim `, `wlang`. También el método general de `render` acepta un bloque.

### Plantillas Inline

Las plantillas pueden ser definidas al final del archivo fuente:

```ruby
require 'sinatra'

get '/' do
  haml :index
end

__END__

@@ layout
%html
  = yield

@@ index
%div.titulo Hola Mundo
```

NOTA: Únicamente las plantillas inline definidas en el archivo fuente que
requiere Sinatra son cargadas automáticamente. Llamá `enable
:inline_templates` explícitamente si tienes plantillas inline en otros
archivos fuente.

### Plantillas Nombradas

Las plantillas también pueden ser definidas usando el método top-level
`template`:

```ruby
template :layout do
  "%html\n  =yield\n"
end

template :index do
  '%div.titulo Hola Mundo!'
end

get '/' do
  haml :index
end
```

Si existe una plantilla con el nombre "layout", va a ser usada cada vez que
una plantilla es renderizada.Puedes desactivar los layouts individualmente
pasando `:layout => false` o globalmente con
`set :haml, :layout => false`:

```ruby
get '/' do
  haml :index, :layout => !request.xhr?
end
```

### Asociando Extensiones de Archivo

Para asociar una extensión de archivo con un motor de renderizado, usa
`Tilt.register`. Por ejemplo, si quieres usar la extensión `tt` para
las plantillas Textile, puedes hacer lo siguiente:

```ruby
Tilt.register :tt, Tilt[:textile]
```

### Añadiendo Tu Propio Motor de Plantillas

Primero, registra tu motor con Tilt, y después, creá tu método de renderizado:

```ruby
Tilt.register :mipg, MiMotorDePlantilla

helpers do
  def mypg(*args) render(:mypg, *args) end
end

get '/' do
  mypg :index
end
```

Renderiza `./views/index.mypg`. Mirá https://github.com/rtomayko/tilt
para aprender más de Tilt.

### Usando Lógica Personalizada para la Búsqueda en Plantillas

Para implementar tu propio mecanismo de búsqueda de plantillas puedes
escribir tu propio método `#find_template`

```ruby
configure do
  set :views [ './views/a', './views/b' ]
end

def find_template(views, name, engine, &block)
  Array(views).each do |v|
    super(v, name, engine, &block)
  end
end
```

## Filtros

Los filtros `before` son evaluados antes de cada petición dentro del mismo
contexto que las rutas. Pueden modificar la petición y la respuesta. Las
variables de instancia asignadas en los filtros son accesibles por las rutas y
las plantillas:

```ruby
before do
  @nota = 'Hey!'
  request.path_info = '/foo/bar/baz'
end

get '/foo/*' do
  @nota #=> 'Hey!'
  params['splat'] #=> 'bar/baz'
end
```

Los filtros `after` son evaluados después de cada petición dentro del mismo
contexto y también pueden modificar la petición y la respuesta. Las variables
de instancia asignadas en los filtros `before` y en las rutas son accesibles por
los filtros `after`:

```ruby
after do
  puts response.status
end
```

Nota: A menos que uses el método `body` en lugar de simplemente devolver un
string desde una ruta, el cuerpo de la respuesta no va a estar disponible en
un filtro after, debido a que todavía no se ha generado.

Los filtros aceptan un patrón opcional, que cuando está presente causa que los
mismos sean evaluados únicamente si el path de la petición coincide con ese
patrón:

```ruby
before '/protegido/*' do
  autenticar!
end

after '/crear/:slug' do |slug|
  session[:ultimo_slug] = slug
end
```

Al igual que las rutas, los filtros también pueden aceptar condiciones:

```ruby
before :agent => /Songbird/ do
  # ...
end

after '/blog/*', :host_name => 'ejemplo.com' do
  # ...
end
```

## Helpers

Usá el método top-level `helpers` para definir métodos ayudantes (helpers) que
pueden ser utilizados dentro de los manejadores de rutas y las plantillas:

```ruby
helpers do
  def bar(nombre)
    "#{nombre}bar"
  end
end

get '/:nombre' do
  bar(params['nombre'])
end
```

Por cuestiones organizativas, puede resultar conveniente organizar los métodos
helpers en distintos módulos:

```ruby
module FooUtils
  def foo(nombre) "#{nombre}foo" end
end

module BarUtils
  def bar(nombre) "#{nombre}bar" end
end

helpers FooUtils, BarUtils
```

El efecto de utilizar `helpers` de esta manera es el mismo que resulta de
incluir los módulos en la clase de la aplicación.

### Usando Sesiones

Una sesión es usada para mantener el estado a través de distintas peticiones.
Cuando están activadas, proporciona un hash de sesión para cada sesión de usuario:

```ruby
enable :sessions

get '/' do
  "valor = " << session[:valor].inspect
end

get '/:valor' do
  session[:valor] = params['valor']
end
```

#### Secreto de Sesión

Para mejorar la seguridad, los datos de la sesión en la cookie se firman con un secreto usando `HMAC-SHA1`. El secreto de esta sesión debería ser de manera óptima
un valor aleatorio criptográficamente seguro de una longitud adecuada para 
`HMAC-SHA1` que es mayor o igual que 64 bytes (512 bits, 128 hex caracteres).
Se le aconsejará que no use un secreto que sea inferior a 32
bytes de aleatoriedad (256 bits, 64 caracteres hexadecimales).
Por lo tanto, es **muy importante** que no solo invente el secreto,
sino que use un generador de números aleatorios para crearlo.
Los humanos somos extremadamente malos generando valores aleatorios

De forma predeterminada, un secreto de sesión aleatorio seguro de 32 bytes se genera para usted por
Sinatra, pero cambiará con cada reinicio de su aplicación. Si tienes varias 
instancias de tu aplicación y dejas que Sinatra genere la clave, cada instancia
tendría una clave de sesión diferente y probablemente no es lo que quieres.

Para una mejor seguridad y usabilidad es 
[recomendado](https://12factor.net/config) que genere un secreto de sesión
aleatorio seguro y se guarde en las variables de entorno en cada host que ejecuta
su aplicación para que todas las instancias de su aplicación compartan el mismo
secreto. Debería rotar periódicamente esta sesión secreta a un nuevo valor.
Aquí hay algunos ejemplos de cómo puede crear un secreto de 64 bytes y configurarlo:

**Generación de Secreto de Sesión**

```text
$ ruby -e "require 'securerandom'; puts SecureRandom.hex(64)"
99ae8af...snip...ec0f262ac
```

**Generación de Secreto de Sesión (Puntos Extras)**

Usa la [gema sysrandom](https://github.com/cryptosphere/sysrandom) para preferir
el uso de el sistema RNG para generar valores aleatorios en lugar de
espacio de usuario `OpenSSL` que MRI Ruby tiene por defecto:

```text
$ gem install sysrandom
Building native extensions.  This could take a while...
Successfully installed sysrandom-1.x
1 gem installed

$ ruby -e "require 'sysrandom/securerandom'; puts SecureRandom.hex(64)"
99ae8af...snip...ec0f262ac
```

**Secreto de Sesión en Variable de Entorno**

Establezca una variable de entorno `SESSION_SECRET` para Sinatra en el valor que
generaste. Haz que este valor sea persistente durante los reinicios de su host. El
método para hacer esto variará a través de los sistemas operativos, esto es para
propósitos ilustrativos solamente:


```bash
# echo "export SESSION_SECRET=99ae8af...snip...ec0f262ac" >> ~/.bashrc
```

**Configuración de la Aplicación y su Secreto de Sesión**

Configura tu aplicación a prueba de fallas si la variable de entorno
`SESSION_SECRET` no esta disponible

Para puntos extras usa la [gema sysrandom](https://github.com/cryptosphere/sysrandom) acá tambien:

```ruby
require 'securerandom'
# -or- require 'sysrandom/securerandom'
set :session_secret, ENV.fetch('SESSION_SECRET') { SecureRandom.hex(64) }
```

#### Configuración de Sesión

Si desea configurarlo más, también puede almacenar un hash con opciones
en la configuración `sessions`:

```ruby
set :sessions, :domain => 'foo.com'
```

Para compartir su sesión en otras aplicaciones en subdominios de foo.com, agregue el prefijo
dominio con un *.* como este en su lugar:

```ruby
set :sessions, :domain => '.foo.com'
```

#### Escogiendo tu Propio Middleware de Sesión

Tenga en cuenta que `enable :sessions` en realidad almacena todos los datos en una cookie.
Esto no siempre podria ser lo que quieres (almacenar muchos datos aumentará su
tráfico, por ejemplo). Puede usar cualquier middleware de sesión proveniente de Rack
para hacerlo, se puede utilizar uno de los siguientes métodos:

```ruby
enable :sessions
set :session_store, Rack::Session::Pool
```

O para configurar sesiones con un hash de opciones:

```ruby
set :sessions, :expire_after => 2592000
set :session_store, Rack::Session::Pool
```

Otra opción es **no** llamar a `enable :sessions`, sino
su middleware de elección como lo haría con cualquier otro middleware.

Es importante tener en cuenta que al usar este método, la protección
de sesiones **no estará habilitada por defecto**.

También será necesario agregar el middleware de Rack para hacer eso:

```ruby
use Rack::Session::Pool, :expire_after => 2592000
use Rack::Protection::RemoteToken
use Rack::Protection::SessionHijacking
```

Mira '[Configurando la protección contra ataques](#configurando-la-protección-contra-ataques)' para mas información.

### Interrupción

Para detener inmediatamente una petición dentro de un filtro o una ruta debes usar:

```ruby
halt
```

También puedes especificar el estado:

```ruby
halt 410
```

O el cuerpo:

```ruby
halt 'esto va a ser el cuerpo'
```

O los dos:

```ruby
halt 401, 'salí de acá!'
```

Con cabeceras:

```ruby
halt 402, { 'Content-Type' => 'text/plain' }, 'venganza'
```

Obviamente, es posible utilizar `halt` con una plantilla:

```ruby
halt erb(:error)
```

### Paso

Una ruta puede pasarle el procesamiento a la siguiente ruta que coincida con
la petición usando `pass`:

```ruby
get '/adivina/:quien' do
  pass unless params['quien'] == 'Franco'
  'Adivinaste!'
end

get '/adivina/*' do
  'Erraste!'
end
```

Se sale inmediatamente del bloque de la ruta y se le pasa el control a la
siguiente ruta que coincida. Si no coincide ninguna ruta, se devuelve 404.

### Desencadenando Otra Ruta

A veces, `pass` no es lo que quieres, sino obtener el
resultado de la llamada a otra ruta. Simplemente use `call` para lograr esto:

```ruby
get '/foo' do
  status, headers, body = call env.merge("PATH_INFO" => '/bar')
  [status, headers, body.map(&:upcase)]
end

get '/bar' do
  "bar"
end
```

Nota que en el ejemplo anterior, es conveniente mover `"bar"` a un
helper, y llamarlo desde `/foo` y `/bar`. Así, vas a simplificar
las pruebas y a mejorar el rendimiento.

Si quieres que la petición se envíe a la misma instancia de la aplicación en
lugar de otra, usá `call!` en lugar de `call`.

En la especificación de Rack puedes encontrar más información sobre
`call`.

### Configurando el Cuerpo, Código de Estado y los Encabezados

Es posible, y se recomienda, asignar el código de estado y el cuerpo de una
respuesta con el valor de retorno de una ruta. Sin embargo, en algunos
escenarios puede que sea conveniente asignar el cuerpo en un punto arbitrario
del flujo de ejecución con el método helper `body`. A partir de ahí, puedes usar ese
mismo método para acceder al cuerpo de la respuesta:

```ruby
get '/foo' do
  body "bar"
end

after do
  puts body
end
```

También es posible pasarle un bloque a `body`, que será ejecutado por el Rack
handler (puedes usar esto para implementar streaming, mira ["Valores de Retorno"](#valores-de-retorno)).

De manera similar, también puedes asignar el código de estado y encabezados:

```ruby
get '/foo' do
  status 418
  headers \
    "Allow"   => "BREW, POST, GET, PROPFIND, WHEN",
    "Refresh" => "Refresh: 20; http://www.ietf.org/rfc/rfc2324.txt"
  body "I'm a tea pot!"
end
```

También, al igual que `body`, `status` y `headers` sin agregarles argumentos pueden usarse
para acceder a sus valores actuales.

### Streaming De Respuestas

A veces vas a querer empezar a enviar la respuesta a pesar de que todavía no
terminaste de generar su cuerpo. También es posible que, en algunos casos,
quieras seguir enviando información hasta que el cliente cierre la conexión.
Cuando esto ocurra, el helper `stream` te va a ser de gran ayuda:

```ruby
get '/' do
  stream do |out|
    out << "Esto va a ser legen -\n"
    sleep 0.5
    out << " (esperalo) \n"
    sleep 1
    out << "- dario!\n"
  end
end
```

Puedes implementar APIs de streaming,
[Server-Sent Events](https://html.spec.whatwg.org/multipage/server-sent-events.html#server-sent-events) y puede ser usado
como base para [WebSockets](https://es.wikipedia.org/wiki/WebSockets). También
puede ser usado para incrementar el throughput si solo una parte del contenido
depende de un recurso lento.

Hay que tener en cuenta que el comportamiento del streaming, especialmente el
número de peticiones concurrentes, depende del servidor web utilizado para
alojar la aplicación. Puede que algunos servidores no soporten streaming
directamente, así el cuerpo de la respuesta será enviado completamente de una
vez cuando el bloque pasado a `stream` finalice su ejecución. Si estás usando
Shotgun, el streaming no va a funcionar.

Cuando se pasa `keep_open` como parámetro, no se va a enviar el mensaje
`close` al objeto de stream. Permite que tu lo cierres en el punto de ejecución
que quieras. Nuevamente, hay que tener en cuenta que este comportamiento es
posible solo en servidores que soporten eventos, como Thin o Rainbows. El
resto de los servidores van a cerrar el stream de todos modos:

```ruby
# long polling

set :server, :thin
connections = []

get '/subscribe' do
  # registrar a un cliente interesado en los eventos del servidor
  stream(:keep_open) do |out|
    connections << out
    # purgar conexiones muertas
    connections.reject!(&:closed?)
  end
end

post '/:message' do
  connections.each do |out|
    # notificar al cliente que ha llegado un nuevo mensaje
    out << params['message'] << "\n"

    # indicar al cliente para conectarse de nuevo
    out.close
  end

  # reconocer
  "message received"
end
```

También es posible que el cliente cierre la conexión cuando intenta
escribir en el socket. Debido a esto, se recomienda verificar con
`out.closed?` antes de intentar escribir.

### Logging

En el ámbito de la petición, el helper `logger` (registrador) expone
una instancia de `Logger`:

```ruby
get '/' do
  logger.info "cargando datos"
  # ...
end
```

Este logger tiene en cuenta la configuración de logueo de tu Rack
handler. Si el logueo está desactivado, este método va a devolver un
objeto que se comporta como un logger pero que en realidad no hace
nada. Así, no vas a tener que preocuparte por esta situación.

Ten en cuenta que el logueo está habilitado por defecto únicamente
para `Sinatra::Application`. Si heredaste de
`Sinatra::Base`, probablemente quieras habilitarlo manualmente:

```ruby
class MiApp < Sinatra::Base
  configure :production, :development do
    enable :logging
  end
end
```

Para evitar que se inicialice cualquier middleware de logging, configurá
`logging` a `nil`. Ten en cuenta que, cuando hagas esto, `logger` va a
devolver `nil`. Un caso común es cuando quieres usar tu propio logger. Sinatra
va a usar lo que encuentre en `env['rack.logger']`.

### Tipos Mime

Cuando usás `send_file` o archivos estáticos tal vez tengas tipos mime
que Sinatra no entiende. Usá `mime_type` para registrarlos a través de la
extensión de archivo:

```ruby
configure do
  mime_type :foo, 'text/foo'
end
```

También lo puedes usar con el helper `content_type`:

```ruby
get '/' do
  content_type :foo
  "foo foo foo"
end
```

### Generando URLs

Para generar URLs deberías usar el método `url`. Por ejemplo, en Haml:

```ruby
%a{:href => url('/foo')} foo
```

Tiene en cuenta proxies inversos y encaminadores de Rack, si están presentes.

Este método también puede invocarse mediante su alias `to` (mirá un ejemplo
a continuación).

### Redirección del Navegador

Puedes redireccionar al navegador con el método `redirect`:

```ruby
get '/foo' do
  redirect to('/bar')
end
```

Cualquier parámetro adicional se utiliza de la misma manera que los argumentos
pasados a `halt`:

```ruby
redirect to('/bar'), 303
redirect 'http://www.google.com/', 'te confundiste de lugar, compañero'
```

También puedes redireccionar fácilmente de vuelta hacia la página desde donde
vino el usuario con `redirect back`:

```ruby
get '/foo' do
  "<a href='/bar'>hacer algo</a>"
end

get '/bar' do
  hacer_algo
  redirect back
end
```

Para pasar argumentos con una redirección, puedes agregarlos a la cadena de
búsqueda:

```ruby
redirect to('/bar?suma=42')
```

O usar una sesión:

```ruby
enable :sessions

get '/foo' do
  session[:secreto] = 'foo'
  redirect to('/bar')
end

get '/bar' do
  session[:secreto]
end
```

### Control del Cache 

Asignar tus encabezados correctamente es el cimiento para realizar un cacheo
HTTP correcto.

Puedes asignar el encabezado Cache-Control fácilmente:

```ruby
get '/' do
  cache_control :public
  "cachealo!"
end
```

Pro tip: configurar el cacheo en un filtro `before`:

```ruby
before do
  cache_control :public, :must_revalidate, :max_age => 60
end
```

Si estás usando el helper `expires` para definir el encabezado correspondiente,
`Cache-Control` se va a definir automáticamente:

```ruby
before do
  expires 500, :public, :must_revalidate
end
```

Para usar cachés adecuadamente, deberías considerar usar `etag` o
`last_modified`. Es recomendable que llames a estos asistentes *antes* de hacer
cualquier trabajo pesado, ya que van a enviar la respuesta inmediatamente si
el cliente ya tiene la versión actual en su caché:

```ruby
get '/articulo/:id' do
  @articulo = Articulo.find params['id']
  last_modified @articulo.updated_at
  etag @articulo.sha1
  erb :articulo
end
```

También es posible usar una
[weak ETag](https://en.wikipedia.org/wiki/HTTP_ETag#Strong_and_weak_validation):

```ruby
etag @articulo.sha1, :weak
```

Estos helpers no van a cachear nada por vos, sino que van a facilitar la
información necesaria para poder hacerlo. Si estás buscando soluciones rápidas
de cacheo con proxys reversos, mirá
[rack-cache](https://github.com/rtomayko/rack-cache):

```ruby
require "rack/cache"
require "sinatra"

use Rack::Cache

get '/' do
  cache_control :public, :max_age => 36000
  sleep 5
  "hola"
end
```

Usá la configuración `:static_cache_control` para agregar el encabezado
`Cache-Control` a archivos estáticos (ver la sección de configuración
para más detalles).

De acuerdo con la RFC 2616 tu aplicación debería comportarse diferente si a las
cabeceras If-Match o If-None-Match se le asigna el valor `*` cuando el
recurso solicitado ya existe. Sinatra asume para peticiones seguras (como get)
y potentes (como put) que el recurso existe, mientras que para el resto
(como post) asume que no. Puedes cambiar este comportamiento con la opción
`:new_resource`:

```ruby
get '/crear' do
  etag '', :new_resource => true
  Articulo.create
  erb :nuevo_articulo
end
```

Si quieres seguir usando una weak ETag, indicalo con la opción `:kind`:

```ruby
etag '', :new_resource => true, :kind => :weak
```

### Enviando Archivos

Para enviar archivos, puedes usar el método `send_file`:

```ruby
get '/' do
  send_file 'foo.png'
end
```

Además acepta un par de opciones:

```ruby
send_file 'foo.png', :type => :jpg
```

Estas opciones son:

<dl>
  <dt>filename</dt>
    <dd>Nombre del archivo devuelto, por defecto es el nombre real del archivo.</dd>
  
  <dt>last_modified</dt>
    <dd>Valor para el encabezado Last-Modified, por defecto toma el mtime del archivo.</dd>

  <dt>type</dt>
    <dd>El Content-Type que se va a utilizar, si no está presente se intenta
        adivinar a partir de la extensión del archivo.</dd>

  <dt>disposition</dt>
    <dd>
      Se utiliza para el encabezado Content-Disposition, y puede tomar alguno de los
      siguientes valores: <tt>nil</tt> (por defecto), <tt>:attachment</tt> y <tt>:inline</tt>
    </dd>

  <dt>length</dt>
    <dd>Encabezado Content-Length, por defecto toma el tamaño del archivo</dd>

  <dt>status</dt>
    <dd>
      Código de estado a enviar. Útil cuando se envía un archivo estático como un error página. Si es compatible con el controlador de Rack, otros medios que no sean la transmisión del proceso de Ruby será utilizado. Si usas este método de ayuda, Sinatra manejará automáticamente las solicitudes de rango.
    </dd>
</dl>

### Accediendo al objeto Request

El objeto de la petición entrante puede ser accedido desde el nivel de la
petición (filtros, rutas y manejadores de errores) a través del método
`request`:

```ruby
# app corriendo en http://ejemplo.com/ejemplo
get '/foo' do
  t = %w[text/css text/html application/javascript]
  request.accept              # ['text/html', '*/*']
  request.accept? 'text/xml'  # true
  request.preferred_type(t)   # 'text/html'
  request.body                # cuerpo de la petición enviado por el cliente (ver más abajo)
  request.scheme              # "http"
  request.script_name         # "/ejemplo"
  request.path_info           # "/foo"
  request.port                # 80
  request.request_method      # "GET"
  request.query_string        # ""
  request.content_length      # longitud de request.body
  request.media_type          # tipo de medio de request.body
  request.host                # "ejemplo.com"
  request.get?                # true (hay métodos análogos para los otros verbos)
  request.form_data?          # false
  request["UNA_CABECERA"]     # valor de la cabecera UNA_CABECERA
  request.referrer            # la referencia del cliente o '/'
  request.user_agent          # user agent (usado por la condición :agent)
  request.cookies             # hash de las cookies del navegador
  request.xhr?                # es una petición ajax?
  request.url                 # "http://ejemplo.com/ejemplo/foo"
  request.path                # "/ejemplo/foo"
  request.ip                  # dirección IP del cliente
  request.secure?             # false (sería true sobre ssl)
  request.forwarded?          # true (si se está corriendo atrás de un proxy reverso)
  request.env                 # hash de entorno directamente entregado por Rack
end
```

Algunas opciones, como `script_name` o `path_info` pueden
también ser escritas:

```ruby
before { request.path_info = "/" }

get "/" do
  "todas las peticiones llegan acá"
end
```

El objeto `request.body` es una instancia de IO o StringIO:

```ruby
post "/api" do
  request.body.rewind  # en caso de que alguien ya lo haya leído
  datos = JSON.parse request.body.read
  "Hola #{datos['nombre']}!"
end
```

### Archivos Adjuntos

Puedes usar el helper `attachment` para indicarle al navegador que
almacene la respuesta en el disco en lugar de mostrarla en pantalla:

```ruby
get '/' do
  attachment
  "guardalo!"
end
```

También puedes pasarle un nombre de archivo:

```ruby
get '/' do
  attachment "info.txt"
  "guardalo!"
end
```

### Fecha y Hora

Sinatra pone a tu disposición el helper `time_for`, que genera un objeto `Time`
a partir del valor que recibe como argumento. Este valor puede ser un
`String`, pero también es capaz de convertir objetos `DateTime`, `Date` y de
otras clases similares:

```ruby
get '/' do
  pass if Time.now > time_for('Dec 23, 2012')
  "todavía hay tiempo"
end
```

Este método es usado internamente por métodos como `expires` y `last_modified`,
entre otros. Por lo tanto, es posible extender el comportamiento de estos
métodos sobreescribiendo `time_for` en tu aplicación:

```ruby
helpers do
  def time_for(value)
    case value
    when :ayer then Time.now - 24*60*60
    when :mañana then Time.now + 24*60*60
    else super
    end
  end
end

get '/' do
  last_modified :ayer
  expires :mañana
  "hola"
end
```

### Buscando los Archivos de las Plantillas

El helper `find_template` se utiliza para encontrar los archivos de las
plantillas que se van a renderizar:

```ruby
find_template settings.views, 'foo', Tilt[:haml] do |archivo|
  puts "podría ser #{archivo}"
end
```

Si bien esto no es muy útil, lo interesante es que puedes sobreescribir este
método, y así enganchar tu propio mecanismo de búsqueda. Por ejemplo, para
poder utilizar más de un directorio de vistas:

```ruby
set :views, ['vistas', 'plantillas']

helpers do
  def find_template(views, name, engine, &block)
    Array(views).each { |v| super(v, name, engine, &block) }
  end
end
```

Otro ejemplo consiste en usar directorios diferentes para los distintos motores
de renderizado:

```ruby
set :views, :sass => 'vistas/sass', :haml => 'plantillas', :defecto => 'vistas'

helpers do
  def find_template(views, name, engine, &block)
    _, folder = views.detect { |k,v| engine == Tilt[k] }
    folder ||= views[:defecto]
    super(folder, name, engine, &block)
  end
end
```

¡Es muy fácil convertir estos ejemplos en una extensión y compartirla!

Notá que `find_template` no verifica si un archivo existe realmente, sino
que llama al bloque que recibe para cada path posible. Esto no representa un
problema de rendimiento debido a que `render` va a usar `break` ni bien
encuentre un archivo que exista. Además, las ubicaciones de las plantillas (y
su contenido) se cachean cuando no estás en el modo de desarrollo. Es bueno
tener en cuenta lo anterior si escribís un método extraño.

## Configuración

Ejecutar una vez, en el inicio, en cualquier entorno:

```ruby
configure do
  # asignando una opción
  set :opcion, 'valor'

  # asignando varias opciones
  set :a => 1, :b => 2

  # atajo para `set :opcion, true`
  enable :opcion

  # atajo para `set :opcion, false`
  disable :opcion

  # también puedes tener configuraciones dinámicas usando bloques
  set(:css_dir) { File.join(views, 'css') }
end
```

Ejecutar únicamente cuando el entorno (la variable de entorno APP_ENV) es
`:production`:

```ruby
configure :production do
  ...
end
```

Ejecutar cuando el entorno es `:production` o `:test`:

```ruby
configure :production, :test do
  ...
end
```

Puedes acceder a estas opciones utilizando el método `settings`:

```ruby
configure do
  set :foo, 'bar'
end

get '/' do
  settings.foo? # => true
  settings.foo  # => 'bar'
  ...
end
```

### Configurando la Protección Contra Ataques

Sinatra usa [Rack::Protection](https://github.com/sinatra/sinatra/tree/master/rack-protection#readme)
para defender a tu aplicación de los ataques más comunes. Si por algún motivo,
quieres desactivar esta funcionalidad, puedes hacerlo como se indica a
continuación (ten en cuenta que tu aplicación va a quedar expuesta a un
montón de vulnerabilidades bien conocidas):

```ruby
disable :protection
```

También es posible desactivar una única capa de defensa:

```ruby
set :protection, :except => :path_traversal
```

O varias:

```ruby
set :protection, :except => [:path_traversal, :session_hijacking]
```

### Configuraciones Disponibles

<dl>
  <dt>absolute_redirects</dt>
  <dd>
    Si está deshabilitada, Sinatra va a permitir
    redirecciones relativas, sin embargo, como consecuencia
    de esto, va a dejar de cumplir con el RFC 2616 (HTTP
    1.1), que solamente permite redirecciones absolutas.
  </dd>
  <dd>
    Activalo si tu apliación está corriendo atrás de un proxy
    reverso que no se ha configurado adecuadamente. Notá que
    el helper <tt>url</tt> va a seguir produciendo URLs absolutas, a
    menos que le pasés <tt>false</tt> como segundo parámetro.
  </dd>
  <dd>Deshabilitada por defecto.</dd>
  </dd>

  <dt>add_charset</dt>
  <dd>
    Tipos mime a los que el helper <tt>content_type</tt> les
    añade automáticamente el charset. En general, no deberías
    asignar directamente esta opción, sino añadirle los charsets
    que quieras:
    <tt>settings.add_charset &lt;&lt; "application/foobar"</tt>
  </dd>

  <dt>app_file</dt>
  <dd>
    Path del archivo principal de la aplicación, se utiliza
    para detectar la raíz del proyecto, el directorio de las
    vistas y el público, así como las plantillas inline.
  </dd>

  <dt>bind</dt>
  <dd>
    Dirección IP que utilizará el servidor integrado (por
    defecto: <tt>0.0.0.0</tt> <em>o</em> <tt>localhost</tt>
    si su `environment` está configurado para desarrollo).
  </dd>

  <dt>default_encoding</dt>
  <dd>
    Encoding utilizado cuando el mismo se desconoce (por
    defecto <tt>"utf-8"</tt>).
  </dd>

  <dt>dump_errors</dt>
  <dd>
    Mostrar errores en el log.
  </dd>

  <dt>environment</dt>
  <dd>
    Entorno actual, por defecto toma el valor de
    <tt>ENV['APP_ENV']</tt>, o <tt>"development"</tt> si no
    está disponible.
  </dd>

  <dt>logging</dt>
  <dd>
    Define si se utiliza el logger.
  </dd>

  <dt>lock</dt>
  <dd>
    Coloca un lock alrededor de cada petición, procesando
    solamente una por proceso.
  </dd>
  <dd>
    Habilitá esta opción si tu aplicación no es thread-safe.
    Se encuentra deshabilitada por defecto.
  </dd>

  <dt>method_override</dt>
  <dd>
    Utiliza el parámetro <tt>_method</tt> para permtir
    formularios put/delete en navegadores que no los
    soportan.
  </dd>
  
  <dt>mustermann_opts</dt>
  <dd>
    Un hash predeterminado de opciones para pasar a Mustermann.new
    al compilar las rutas.
  </dd>
  
  <dt>port</dt>
  <dd>
    Puerto en el que escuchará el servidor integrado.
  </dd>

  <dt>prefixed_redirects</dt>
  <dd>
    Define si inserta <tt>request.script_name</tt> en las
    redirecciones cuando no se proporciona un path absoluto.
    De esta manera, cuando está habilitada,
    <tt>redirect '/foo'</tt> se comporta de la misma manera
    que <tt>redirect to('/foo')</tt>. Se encuentra
    deshabilitada por defecto.
  </dd>

  <dt>protection</dt>
  <dd>
    Define si se habilitan o no las protecciones de ataques web. 
    Ver la sección de protección encima.
  </dd>

  <dt>public_folder</dt>
  <dd>
    Lugar del directorio desde donde se sirven los archivos
    públicos. Solo se utiliza cuando se sirven archivos
    estáticos (ver la opción <tt>static</tt>). Si no
    está presente, se infiere del valor de la opción
    <tt>app_file</tt>.
  </dd>
  
  <dt>quiet</dt>
  <dd>
    Inhabilita los logs generados por los comandos de inicio y detención de Sinatra.
    <tt> false </tt> por defecto.
  </dd>

  <dt>reload_templates</dt>
  <dd>
    Define Si se vuelven a cargar plantillas entre las solicitudes o no. Habilitado en
    modo de desarrollo.
  </dd>

  <dt>root</dt>
  <dd>
    Lugar del directorio raíz del proyecto. Si no está
    presente, se infiere del valor de la opción
    <tt>app_file</tt>.
  </dd>

  <dt>raise_errors</dt>
  <dd>
    Elevar excepciones (detiene la aplicación). Se
    encuentra activada por defecto cuando el valor de
    <tt>environment</tt>  es <tt>"test"</tt>. En caso
    contrario estará desactivada.
  </dd>

  <dt>run</dt>
  <dd>
    Cuando está habilitada, Sinatra se va a encargar de
    iniciar el servidor web, no la habilites cuando estés
    usando rackup o algún otro medio.
  </dd>

  <dt>running</dt>
  <dd>
    Indica si el servidor integrado está ejecutándose, ¡no
    cambiés esta configuración!.
  </dd>

  <dt>server</dt>
  <dd>
    Servidor, o lista de servidores, para usar como servidor
    integrado. Por defecto: <tt>['thin', 'mongrel', 'webrick']</tt>,
    el orden establece la prioridad.
  </dd>
  
  <dt>server_settings</dt>
  <dd>
    Si está utilizando un servidor web WEBrick, presumiblemente para su entorno de desarrollo, puede pasar un hash de opciones a <tt> server_settings </tt>, como <tt> SSLEnable </tt> o <tt> SSLVerifyClient </tt>. Sin embargo, los servidores web como Puma y Thin no son compatibles, por lo que puede establecer <tt> server_settings </tt> definiéndolo como un método cuando llame a <tt> configure </tt>.
  </dd>

  <dt>sessions</dt>
  <dd>
    Habilita el soporte de sesiones basadas en cookies a
    través de <tt>Rack::Session::Cookie</tt>. Ver la
    sección 'Usando Sesiones' para más información.
  </dd>
  
<dt>session_store</dt>
<dd>
Define el middleware de sesión Rack utilizado. Predeterminado a
<tt>Rack::Session::Cookie</tt>. Consulte la sección 'Uso de sesiones' para obtener más información.
información.
</dd>

  <dt>show_exceptions</dt>
  <dd>
    Muestra un stack trace en el navegador cuando ocurre una
    excepción. Se encuentra activada por defecto cuando el
    valor de <tt>environment</tt> es <tt>"development"</tt>.
    En caso contrario estará desactivada.
  </dd>
  <dd>
     También se puede establecer en <tt> :after_handler </tt> para activar la aplicación especificada
     que hara el manejo de errores antes de mostrar un stack trace en el navegador.
  </dd>

  <dt>static</dt>
  <dd>
     <dd> Define si  Sinatra debe servir los archivos estáticos. </dd>
     <dd> Deshabilitar cuando se utiliza un servidor capaz de hacer esto por su cuenta. </dd>
     <dd> La desactivación aumentará el rendimiento. </dd>
     <dd>
       Habilitado por defecto en estilo clásico, deshabilitado para aplicaciones modulares.
     </dd>
  </dd>

  <dt>static_cache_control</dt>
  <dd>
    Cuando Sinatra está sirviendo archivos estáticos, y
    esta opción está habilitada, les va a agregar encabezados
    <tt>Cache-Control</tt> a las respuestas. Para esto
    utiliza el helper <tt>cache_control</tt>. Se encuentra
    deshabilitada por defecto. Notar que es necesario
    utilizar un array cuando se asignan múltiples valores:
    <tt>set :static_cache_control, [:public, :max_age => 300]</tt>.
  </dd>
  
<dt>threaded</dt>
<dd>
Si se establece en <tt> true </tt>, le dirá a Thin que use
<tt> EventMachine.defer </tt> para procesar la solicitud.
</dd>

<dt> traps </dt>
<dd> Define si Sinatra debería manejar las señales del sistema. </dd>

  <dt>views</dt>
  <dd>
    Path del directorio de las vistas. Si no está presente,
    se infiere del valor de la opción <tt>app_file</tt>.
  </dd>
<dt>x_cascade</dt>
  <dd>
    Establece o no el encabezado de X-Cascade si no hay una coincidencia de ruta.
    <tt> verdadero </tt> por defecto.
  </dd>
</dl>

## Entornos

Existen tres entornos (`environments`) predefinidos: `development`,
`production` y `test`. El entorno por defecto es
`development` y tiene algunas particularidades:

* Se recargan las plantillas entre una petición y la siguiente, a diferencia
de `production` y `test`, donde se cachean.
* Se instalan manejadores de errores `not_found` y `error`
especiales que muestran un stack trace en el navegador cuando son disparados.

Para utilizar alguno de los otros entornos puede asignarse el valor
correspondiente a la variable de entorno `APP_ENV`:
```shell
APP_ENV=production ruby my_app.rb
```

Los métodos `development?`, `test?` y `production?` te permiten conocer el
entorno actual.

```ruby
get '/' do
  if settings.development?
    "development!"
  else
    "not development!"
  end
end
```

## Manejo de Errores

Los manejadores de errores se ejecutan dentro del mismo contexto que las rutas
y los filtros `before`, lo que significa que puedes usar, por ejemplo,
`haml`, `erb`, `halt`, etc.

### Not Found

Cuando se eleva una excepción `Sinatra::NotFound`, o el código de
estado de la respuesta es 404, el manejador `not_found` es invocado:

```ruby
not_found do
  'No existo'
end
```

### Error

El manejador `error` es invocado cada vez que una excepción es elevada
desde un bloque de ruta o un filtro. El objeto de la excepción se puede
obtener de la variable Rack `sinatra.error`:

```ruby
error do
  'Disculpá, ocurrió un error horrible - ' + env['sinatra.error'].message
end
```

Errores personalizados:

```ruby
error MiErrorPersonalizado do
  'Lo que pasó fue...' + env['sinatra.error'].message
end
```

Entonces, si pasa esto:

```ruby
get '/' do
  raise MiErrorPersonalizado, 'algo malo'
end
```

Obtienes esto:

```
  Lo que pasó fue... algo malo
```

También, puedes instalar un manejador de errores para un código de estado:

```ruby
error 403 do
  'Acceso prohibido'
end

get '/secreto' do
  403
end
```

O un rango:

```ruby
error 400..510 do
  'Boom'
end
```

Sinatra instala manejadores `not_found` y `error` especiales
cuando se ejecuta dentro del entorno de desarrollo "development" y se muestran
en tu navegador para que tengas información adicional sobre el error.

## Rack Middleware

Sinatra corre sobre [Rack](http://rack.github.io/), una interfaz minimalista
que es un estándar para frameworks webs escritos en Ruby. Una de las
características más interesantes de Rack para los desarrolladores de aplicaciones
es el soporte de "middleware" -- componentes que se ubican entre el servidor y
tu aplicación, supervisando y/o manipulando la petición/respuesta HTTP para
proporcionar varios tipos de funcionalidades comunes.

Sinatra hace muy sencillo construir tuberías de Rack middleware a través del
método top-level `use`:

```ruby
require 'sinatra'
require 'mi_middleware_personalizado'

use Rack::Lint
use MiMiddlewarePersonalizado

get '/hola' do
  'Hola Mundo'
end
```

La semántica de `use` es idéntica a la definida para el DSL
[Rack::Builder](http://www.rubydoc.info/github/rack/rack/master/Rack/Builder) (más
frecuentemente usado en archivos rackup). Por ejemplo, el método `use`
acepta argumentos múltiples/variables así como bloques:

```ruby
use Rack::Auth::Basic do |nombre_de_usuario, password|
  nombre_de_usuario == 'admin' && password == 'secreto'
end
```

Rack es distribuido con una variedad de middleware estándar para logging,
debugging, enrutamiento URL, autenticación y manejo de sesiones. Sinatra
usa muchos de estos componentes automáticamente de acuerdo a su configuración
para que usualmente no tengas que usarlas (con `use`) explícitamente.

Puedes encontrar middleware útil en
[rack](https://github.com/rack/rack/tree/master/lib/rack),
[rack-contrib](https://github.com/rack/rack-contrib#readme),
o en la [Rack wiki](https://github.com/rack/rack/wiki/List-of-Middleware).

## Pruebas

Las pruebas para las aplicaciones Sinatra pueden ser escritas utilizando
cualquier framework o librería de pruebas basada en Rack. Se recomienda usar
[Rack::Test](http://www.rubydoc.info/github/brynary/rack-test/master/frames):

```ruby
require 'mi_app_sinatra'
require 'minitest/autorun'
require 'rack/test'

class MiAppTest < Minitest::Test
  include Rack::Test::Methods

  def app
    Sinatra::Application
  end

  def test_mi_defecto
    get '/'
    assert_equal 'Hola Mundo!', last_response.body
  end

  def test_con_parametros
    get '/saludar', :name => 'Franco'
    assert_equal 'Hola Frank!', last_response.body
  end

  def test_con_entorno_rack
    get '/', {}, 'HTTP_USER_AGENT' => 'Songbird'
    assert_equal "Estás usando Songbird!", last_response.body
  end
end
```

Nota: Si está utilizando Sinatra en el estilo modular, reemplace
`Sinatra::Application` anterior con el nombre de clase de su aplicación.

## Sinatra::Base - Middleware, Librerías, y Aplicaciones Modulares

Definir tu aplicación en el nivel superior funciona bien para micro-aplicaciones
pero trae inconvenientes considerables a la hora de construir componentes
reutilizables como Rack middleware, Rails metal, librerías simples con un
componente de servidor o incluso extensiones de Sinatra. El DSL de alto nivel
asume una configuración apropiada para micro-aplicaciones (por ejemplo, un
único archivo de aplicación, los directorios `./public` y
`./views`, logging, página con detalles de excepción, etc.). Ahí es
donde `Sinatra::Base` entra en el juego:

```ruby
require 'sinatra/base'

class MiApp < Sinatra::Base
  set :sessions, true
  set :foo, 'bar'

  get '/' do
    'Hola Mundo!'
  end
end
```

Las subclases de `Sinatra::Base` tienen disponibles exactamente los
mismos métodos que los provistos por el DSL de top-level. La mayoría de las
aplicaciones top-level se pueden convertir en componentes
`Sinatra::Base` con dos modificaciones:

* Tu archivo debe requerir `sinatra/base` en lugar de `sinatra`; de otra
  manera, todos los métodos del DSL de sinatra son importados dentro del
  espacio de nombres principal.
* Poné las rutas, manejadores de errores, filtros y opciones de tu aplicación
  en una subclase de `Sinatra::Base`.

`Sinatra::Base` es una pizarra en blanco. La mayoría de las opciones están
desactivadas por defecto, incluyendo el servidor incorporado. Mirá
[Opciones y Configuraciones](http://www.sinatrarb.com/configuration.html)
para detalles sobre las opciones disponibles y su comportamiento.
Si quieres un comportamiento más similar
a cuando defines tu aplicación en el nivel superior (también conocido como Clásico)
estilo), puede subclase `Sinatra::Aplicación`

```ruby
require 'sinatra/base'

class MiAplicacion < Sinatra::Application
  get '/' do
    'Hola Mundo!'
  end
end
```

### Estilo Modular vs. Clásico

Contrariamente a la creencia popular, no hay nada de malo con el estilo clásico.
Si se ajusta a tu aplicación, no es necesario que la cambies a una modular.

La desventaja de usar el estilo clásico en lugar del modular consiste en que
solamente puedes tener una aplicación Sinatra por proceso Ruby. Si tienes
planificado usar más, cambiá al estilo modular. Al mismo tiempo, ten en
cuenta que no hay ninguna razón por la cuál no puedas mezclar los estilos
clásico y modular.

A continuación se detallan las diferencias (sútiles) entre las configuraciones
de ambos estilos:

<table>
  <tr>
    <th>Configuración</th>
    <th>Clásica</th>
    <th>Modular</th>
    <th>Modular</th>
  </tr>

  <tr>
    <td>app_file</td>
    <td>archivo que carga sinatra</td>
    <td>archivo con la subclase de Sinatra::Base</td>
    <td>archivo con la subclase Sinatra::Application</td>
  </tr>

  <tr>
    <td>run</td>
    <td>$0 == app_file</td>
    <td>false</td>
    <td>false</td>
  </tr>

  <tr>
    <td>logging</td>
    <td>true</td>
    <td>false</td>
    <td>true</td>
  </tr>

  <tr>
    <td>method_override</td>
    <td>true</td>
    <td>false</td>
    <td>true</td>
  </tr>

  <tr>
    <td>inline_templates</td>
    <td>true</td>
    <td>false</td>
    <td>true</td>
  </tr>

  <tr>
    <td>static</td>
    <td>true</td>
    <td>File.exist?(public_folder)</td>
    <td>true</td>
  </tr>
</table>

### Sirviendo una Aplicación Modular

Las dos opciones más comunes para iniciar una aplicación modular son, iniciarla
activamente con `run!`:

```ruby
# mi_app.rb
require 'sinatra/base'

class MiApp < Sinatra::Base
  # ... código de la app  ...

  # iniciar el servidor si el archivo fue ejecutado directamente
  run! if app_file == $0
end
```

Iniciar con:

```shell
ruby mi_app.rb
```

O, con un archivo `config.ru`, que permite usar cualquier handler Rack:

```ruby
# config.ru
require './mi_app'
run MiApp
```

Después ejecutar:

```shell
rackup -p 4567
```

### Usando una Aplicación Clásica con un Archivo config.ru

Escribí el archivo de tu aplicación:

```ruby
# app.rb
require 'sinatra'

get '/' do
  'Hola mundo!'
end
```

Y el `config.ru` correspondiente:

```ruby
require './app'
run Sinatra::Application
```

### ¿Cuándo usar config.ru?

Indicadores de que probablemente quieres usar `config.ru`:

* Quieres realizar el deploy con un handler Rack distinto (Passenger, Unicorn,
  Heroku, ...).
* Quieres usar más de una subclase de `Sinatra::Base`.
* Quieres usar Sinatra únicamente para middleware, pero no como un endpoint.

<b>No hay necesidad de utilizar un archivo `config.ru` exclusivamente
porque tienes una aplicación modular, y no necesitás una aplicación modular para
iniciarla con `config.ru`.</b>

### Utilizando Sinatra como Middleware

Sinatra no solo es capaz de usar otro Rack middleware, sino que a su vez,
cualquier aplicación Sinatra puede ser agregada delante de un endpoint Rack
como middleware. Este endpoint puede ser otra aplicación Sinatra, o cualquier
aplicación basada en Rack (Rails/Ramaze/Camping/...):

```ruby
require 'sinatra/base'

class PantallaDeLogin < Sinatra::Base
  enable :sessions

  get('/login') { haml :login }

  post('/login') do
    if params['nombre'] == 'admin' && params['password'] == 'admin'
      session['nombre_de_usuario'] = params['nombre']
    else
      redirect '/login'
    end
  end
end

class MiApp < Sinatra::Base
  # el middleware se ejecutará antes que los filtros
  use PantallaDeLogin

  before do
    unless session['nombre_de_usuario']
      halt "Acceso denegado, por favor <a href='/login'>iniciá sesión</a>."
    end
  end

  get('/') { "Hola #{session['nombre_de_usuario']}." }
end
```

### Creación Dinámica de Aplicaciones

Puede que en algunas ocasiones quieras crear nuevas aplicaciones en
tiempo de ejecución sin tener que asignarlas a una constante. Para
esto tienes `Sinatra.new`:

```ruby
require 'sinatra/base'
mi_app = Sinatra.new { get('/') { "hola" } }
mi_app.run!
```

Acepta como argumento opcional una aplicación desde la que se
heredará:

```ruby
# config.ru
require 'sinatra/base'

controller = Sinatra.new do
  enable :logging
  helpers MisHelpers
end

map('/a') do
  run Sinatra.new(controller) { get('/') { 'a' } }
end

map('/b') do
  run Sinatra.new(controller) { get('/') { 'b' } }
end
```

Construir aplicaciones de esta forma resulta especialmente útil para
testear extensiones Sinatra o para usar Sinatra en tus librerías.

Por otro lado, hace extremadamente sencillo usar Sinatra como
middleware:

```ruby
require 'sinatra/base'

use Sinatra do
  get('/') { ... }
end

run ProyectoRails::Application
```

## Ámbitos y Ligaduras

El ámbito en el que te encuentras determina que métodos y variables están
disponibles.

### Ámbito de Aplicación/Clase

Cada aplicación Sinatra es una subclase de `Sinatra::Base`. Si estás
usando el DSL de top-level (`require 'sinatra'`), entonces esta clase es
`Sinatra::Application`, de otra manera es la subclase que creaste
explícitamente. Al nivel de la clase tienes métodos como `get` o `before`, pero
no puedes acceder a los objetos `request` o `session`, ya que hay una única
clase de la aplicación para todas las peticiones.

Las opciones creadas utilizando `set` son métodos al nivel de la clase:

```ruby
class MiApp < Sinatra::Base
  # Ey, estoy en el ámbito de la aplicación!
  set :foo, 42
  foo # => 42

  get '/foo' do
    # Hey, ya no estoy en el ámbito de la aplicación!
  end
end
```

Tienes la ligadura al ámbito de la aplicación dentro de:

* El cuerpo de la clase de tu aplicación
* Métodos definidos por extensiones
* El bloque pasado a `helpers`
* Procs/bloques usados como el valor para `set`
* El bloque pasado a `Sinatra.new`

Este ámbito puede alcanzarse de las siguientes maneras:

* A través del objeto pasado a los bloques de configuración (`configure { |c| ...}`)
* Llamando a `settings` desde dentro del ámbito de la petición

### Ámbito de Petición/Instancia

Para cada petición entrante, una nueva instancia de la clase de tu aplicación
es creada y todos los bloques de rutas son ejecutados en ese ámbito. Desde este
ámbito puedes acceder a los objetos `request` y `session` o llamar a los métodos
de renderización como `erb` o `haml`. Puedes acceder al ámbito de la aplicación
desde el ámbito de la petición utilizando `settings`:

```ruby
class MiApp < Sinatra::Base
  # Ey, estoy en el ámbito de la aplicación!
  get '/definir_ruta/:nombre' do
    # Ámbito de petición para '/definir_ruta/:nombre'
    @valor = 42

    settings.get("/#{params['nombre']}") do
      # Ámbito de petición para "/#{params['nombre']}"
      @valor # => nil (no es la misma petición)
    end

    "Ruta definida!"
  end
end
```

Tienes la ligadura al ámbito de la petición dentro de:

* bloques pasados a get, head, post, put, delete, options, patch, link y unlink
* filtros before/after
* métodos helpers
* plantillas/vistas

### Ámbito de Delegación

El ámbito de delegación solo reenvía métodos al ámbito de clase. De cualquier
manera, no se comporta 100% como el ámbito de clase porque no tienes la ligadura
de la clase: únicamente métodos marcados explícitamente para delegación están
disponibles y no compartís variables/estado con el ámbito de clase (léase:
tienes un `self` diferente). Puedes agregar delegaciones de método llamando a
`Sinatra::Delegator.delegate :nombre_del_metodo`.

Tienes és la ligadura al ámbito de delegación dentro de:

* La ligadura del top-level, si hiciste `require "sinatra"`
* Un objeto extendido con el mixin `Sinatra::Delegator`

Hechale un vistazo al código: acá está el
[Sinatra::Delegator mixin](https://github.com/sinatra/sinatra/blob/ca06364/lib/sinatra/base.rb#L1609-1633)
que [extiende el objeto main](https://github.com/sinatra/sinatra/blob/ca06364/lib/sinatra/main.rb#L28-30).

## Línea de Comandos

Las aplicaciones Sinatra pueden ser ejecutadas directamente:

```shell
ruby myapp.rb [-h] [-x] [-q] [-e ENVIRONMENT] [-p PORT] [-o HOST] [-s HANDLER]
```

Las opciones son:

```
-h # ayuda
-p # asigna el puerto (4567 es usado por defecto)
-o # asigna el host (0.0.0.0 es usado por defecto)
-e # asigna el entorno (development es usado por defecto)
-s # especifica el servidor/manejador rack (thin es usado por defecto)
-q # activar el modo silecioso para el servidor (está desactivado por defecto)
-x # activa el mutex lock (está desactivado por defecto)
```

### Multi-threading

_Basado en [esta respuesta en StackOverflow](http://stackoverflow.com/questions/6278817/is-sinatra-multi-threaded/6282999#6282999) escrita por Konstantin_

Sinatra no impone ningún modelo de concurrencia, sino que lo deja en manos del
handler Rack que se esté usando (Thin, Puma, WEBrick). Sinatra en sí mismo es
thread-safe, así que no hay problema en que el Rack handler use un modelo de
concurrencia basado en hilos.

Esto significa que, cuando estemos arrancando el servidor, tendríamos que
especificar la opción adecuada para el handler Rack específico. En este ejemplo
vemos cómo arrancar un servidor Thin multihilo:

```ruby
# app.rb

require 'sinatra/base'

class App < Sinatra::Base
  get '/' do
    "¡Hola, Mundo!"
  end
end

App.run!
```

Para arrancar el servidor, el comando sería:

```shell
thin --threaded start
```

## Requerimientos

Las siguientes versiones de Ruby son soportadas oficialmente:

<dl>
  <dt>Ruby 2.2</dt>
    <dd>
      2.2 Es totalmente compatible y recomendado. Actualmente no hay planes
      soltar el apoyo oficial para ello.
    </dd>

  <dt>Rubinius</dt>
  <dd>
    Rubinius es oficialmente compatible (Rubinius> = 2.x). Se recomienda instalar la gema puma 
    <tt>gem install puma</tt>.
  </dd>

  <dt>JRuby</dt>
  <dd>
    La última versión estable de JRuby es oficialmente compatible. No lo es
    recomienda usar extensiones C con JRuby. Se recomienda instalar la gema trinidad
    <tt> gem install trinidad </tt>.
  </dd>
</dl>

Las versiones de Ruby anteriores a 2.2.2 ya no son compatibles con Sinatra 2.0 . 

Siempre le prestamos atención a las nuevas versiones de Ruby.

Las siguientes implementaciones de Ruby no se encuentran soportadas
oficialmente. De cualquier manera, pueden ejecutar Sinatra:

* Versiones anteriores de JRuby y Rubinius
* Ruby Enterprise Edition
* MacRuby, Maglev e IronRuby
* Ruby 1.9.0 y 1.9.1 (pero no te recomendamos que los uses)

No ser soportada oficialmente, significa que si las cosas se rompen
ahí y no en una plataforma soportada, asumimos que no es nuestro problema sino
el suyo.

También ejecutamos nuestro CI contra ruby-head (futuras versiones de MRI), pero
no puede garantizar nada, ya que se mueve constantemente. Esperar próxima
2.x versiones para ser totalmente compatibles.

Sinatra debería trabajar en cualquier sistema operativo compatible la implementación de Ruby
elegida

Si ejecuta MacRuby, debe `gem install control_tower`.

Sinatra actualmente no funciona con Cardinal, SmallRuby, BlueRuby o cualquier
versión de Ruby anterior a 2.2.

## A la Vanguardia

Si quieres usar el código de Sinatra más reciente, sientete libre de ejecutar
tu aplicación sobre la rama master, en general es bastante estable.

También liberamos prereleases de vez en cuando, así, puedes hacer:

```shell
gem install sinatra --pre
```

Para obtener algunas de las últimas características.

### Usando Bundler

Esta es la manera recomendada para ejecutar tu aplicación sobre la última
versión de Sinatra usando [Bundler](http://bundler.io).

Primero, instala Bundler si no lo hiciste todavía:

```shell
gem install bundler
```

Después, en el directorio de tu proyecto, creá un archivo `Gemfile`:

```ruby
source :rubygems
gem 'sinatra', :git => "git://github.com/sinatra/sinatra.git"

# otras dependencias
gem 'haml'                    # por ejemplo, si usás haml
```

Ten en cuenta que tienes que listar todas las dependencias directas de tu
aplicación. No es necesario listar las dependencias de Sinatra (Rack y Tilt)
porque Bundler las agrega directamente.

Ahora puedes arrancar tu aplicación así:

```shell
bundle exec ruby miapp.rb
```

## Versionado

Sinatra utiliza el [Versionado Semántico](http://semver.org/),
siguiendo las especificaciones SemVer y SemVerTag.

## Lecturas Recomendadas

* [Sito web del proyecto](http://www.sinatrarb.com/) - Documentación
  adicional, noticias, y enlaces a otros recursos.
* [Contribuyendo](http://www.sinatrarb.com/contributing) - ¿Encontraste un
  error?. ¿Necesitas ayuda?. ¿Tienes un parche?.
* [Seguimiento de problemas](https://github.com/sinatra/sinatra/issues)
* [Twitter](https://twitter.com/sinatra)
* [Lista de Correo](http://groups.google.com/group/sinatrarb/topics)
* [IRC: #sinatra](irc://chat.freenode.net/#sinatra) en http://freenode.net
* [Sinatra & Friends](https://sinatrarb.slack.com) en Slack y revisa
  [acá](https://sinatra-slack.herokuapp.com/) Para una invitación.
* [Sinatra Book](https://github.com/sinatra/sinatra-book/) Tutorial (en inglés).
* [Sinatra Recipes](http://recipes.sinatrarb.com/) Recetas contribuidas
  por la comunidad (en inglés).
* Documentación de la API para la
  [última versión liberada](http://www.rubydoc.info/gems/sinatra) o para la
  [rama de desarrollo actual](http://www.rubydoc.info/github/sinatra/sinatra)
  en http://www.rubydoc.info/
* [Servidor de CI](https://travis-ci.org/sinatra/sinatra)
